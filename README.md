# usb-dra

A Kubernetes [DRA](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/)
driver (`usb.jacaudi.dev`) that publishes the USB devices attached to each node as schedulable
devices, the way a GPU is exposed. A pod asks for "a radio" or "the LoRa board", or pins one
physical device by serial or port, instead of hardcoding a per-device resource name.

The driver does not decide what a device is. It publishes every eligible device with stable
identity attributes; a `DeviceClass` CEL selector on vendor and product defines a device type.

## What it publishes

A DaemonSet scans `/sys/bus/usb/devices` on every node and publishes one device per USB
device, except:

- **Hubs.** Any device with `bDeviceClass` `09`, root hubs included. No flag overrides this.
- **USB/IP devices.** Anything behind a `vhci_hcd` controller.
- **Excluded classes.** A device is hidden when every one of its interface classes is in the
  excluded set. The default is `03,08,e0` (HID, mass storage, wireless controller), so a
  keyboard or a Bluetooth dongle is never claimable by accident. Only interface classes
  count, never `bDeviceClass`: real devices report `00` or `ef` there.
- **Unconfigured devices** with no interface entries, and devices whose attribute files
  cannot be read mid-enumeration, or whose root hub speed is unknown. These are skipped for
  that scan and logged at `-v=2`; an entry missing one of its identity files is dropped
  silently.

Two flags tune the filter:

| Flag | Default | Meaning |
| --- | --- | --- |
| `--exclude-classes` | `03,08,e0` | Comma-separated interface classes, each two lowercase hex digits. Replaces the default; an empty value publishes everything except hubs and vhci devices. |
| `--include` | none | Repeatable `vid:pid` or `vid:pid:serial` (four lowercase hex digits each; the serial may contain `:` and compares case-insensitively). A matching device is published whatever its interface classes. |

An invalid value for either flag is a startup error. The other flags are `--driver-name`,
`--node-name` (default `$NODE_NAME`), `--cdi-root`, `--kubelet-registrar-dir`,
`--kubelet-plugin-dir`, `--sysfs-root` and `--rescan-interval`; see `--help`.

A node publishes at most 128 devices (the ResourceSlice limit); any beyond that are not
published and an error is logged.

A node with no eligible devices still publishes an empty pool. "Pool with no devices" tells
the scheduler the driver is healthy and has nothing to offer, which differs from "no pool at
all", and it avoids leaving a stale slice behind when the last device is unplugged.

## Identity

Every device gets a name `usb-<vid>-<pid>-<id>`, lowercased and sanitized to a DNS label (a
name over 63 characters is truncated and suffixed with a short hash).

- **Serial identity.** The device has a usable serial: non-empty, valid UTF-8, at most 64
  bytes, free of control characters, and not all zeros. `<id>` is the serial, for example `usb-0bda-2838-35650102`.
- **Port identity.** Everything else. `<id>` is `p-<port>`, for example
  `usb-1a86-7523-p-0000-00-14-0-v2-1-4`.

The `identity` attribute says which one a device has (`serial` or `port`).

**Duplicate serials.** If two devices end up with the same name (a shared serial, or serials
that are equal after case-folding or sanitizing), every one of them falls back to port
identity and the duplicate is logged as an error naming the serial. The `serial` attribute is
still published on them, but their `identity` is `port`, so a claim pinned with the shipped
serial idiom (`identity == "serial" && serial == "..."`) stops matching either device rather
than matching one of them. An already prepared claim keeps its spec until its next prepare,
which then fails with "no longer attached". Fix the serials and replug.

RTL-SDR sticks ship with the factory serial `00000001`. Reprogram one with
`rtl_eeprom -s <new-serial>` and **physically replug**; the EEPROM is only re-read at
power-up, so re-enumeration alone is not enough.

Swapping two identical port-identity devices between ports swaps their identities with no
error; the `identity` attribute exists so a claim author can see this coming.

## Attributes

All attributes are strings under `device.attributes["usb.jacaudi.dev"]`.

| Key | When | Bound | Example |
| --- | --- | --- | --- |
| `vendor` | always | 4 chars | `239a` |
| `product` | always | 4 chars | `8029` |
| `identity` | always | | `serial` or `port` |
| `port` | always; a device whose port exceeds 64 bytes is skipped and logged | 64 bytes | `PNP0D10:02-v2-1` |
| `serial` | when non-empty, valid UTF-8, at most 64 bytes, free of control characters and not all zeros (duplicates included) | 64 bytes | `FD14948954CD1F4F` |
| `manufacturer` | when reported and valid UTF-8; truncated on a rune boundary | 64 bytes | `RAKwireless` |
| `productName` | same as `manufacturer` | 64 bytes | `WisCore RAK4631 Board` |
| `interfaceClasses` | when at most 64 bytes; sorted unique, comma-joined | 64 bytes | `02,0a` |

`port` is `<controller>-v<revision>-<port chain>`: the controller is the root hub's `serial`
(the kernel sets it to the controller's bus name), the revision is the USB generation derived
from the bus speed, and the chain is the device's path below the bus. The bus number is never
part of it. A SuperSpeed device that falls back to high speed changes revision and therefore
port.

Never published: the device node, bus number, device number, tty names and tty aliases. They
change on every re-enumeration, so a claim written against them could silently select the
wrong device after a replug.

## Selecting devices

An **unpinned** claim draws any free device of its DeviceClass. A **CEL selector** on the
request pins one specific device by serial or by port. A device is allocated to at most one
claim at a time. Use a ResourceClaimTemplate so each pod gets its own claim; the scheduler
places the pod on the node that holds the allocated device.

Two things make a CEL selector safe or not:

- A reference to an attribute a device lacks aborts allocation ("no such key"), and class
  selectors are evaluated against devices of every driver.
- So: every DeviceClass selector starts with `device.driver == "usb.jacaudi.dev"` joined to
  the rest with `&&`; pin by serial with `identity == "serial" && serial == "<serial>"`; pin by
  port with `port == "<port>"` (`port` is always present); and reference `serial`,
  `manufacturer`, `productName` and `interfaceClasses` only behind `identity == "serial"` (for
  `serial`) or `has(...)`.

CEL has no alias for the attribute map, so selectors spell it out. The chart renders the
DeviceClasses from its `deviceClasses` values; the example deployment in
`examples/values.yaml` defines the two used below (see [Install](#install)), and the example
claims are in `examples/claims.yaml`. The shipped examples:

Any RTL-SDR (the DeviceClass is the `rtl-sdr` class from `examples/values.yaml`; the claim
template is in `examples/claims.yaml`):

```yaml
apiVersion: resource.k8s.io/v1
kind: DeviceClass
metadata:
  name: rtl-sdr.usb.jacaudi.dev
spec:
  selectors:
    - cel:
        expression: >-
          device.driver == "usb.jacaudi.dev" &&
          device.attributes["usb.jacaudi.dev"].vendor == "0bda" &&
          device.attributes["usb.jacaudi.dev"].product in ["2838", "2832"]
---
apiVersion: resource.k8s.io/v1
kind: ResourceClaimTemplate
metadata:
  name: rtl-sdr-any
  namespace: selfhosted
spec:
  spec:
    devices:
      requests:
        - name: radio
          exactly:
            deviceClassName: rtl-sdr.usb.jacaudi.dev
            allocationMode: ExactCount
            count: 1
```

One RTL-SDR pinned by serial:

```yaml
apiVersion: resource.k8s.io/v1
kind: ResourceClaimTemplate
metadata:
  name: rtl-sdr-35650102
  namespace: selfhosted
spec:
  spec:
    devices:
      requests:
        - name: radio
          exactly:
            deviceClassName: rtl-sdr.usb.jacaudi.dev
            allocationMode: ExactCount
            count: 1
            selectors:
              - cel:
                  expression: >-
                    device.attributes["usb.jacaudi.dev"].identity == "serial" &&
                    device.attributes["usb.jacaudi.dev"].serial == "35650102"
```

The RAK4631 LoRa board pinned by port (it has a usable serial, but the port pin also works
for devices that have none). The DeviceClass is the `rak4631` class from
`examples/values.yaml`:

```yaml
apiVersion: resource.k8s.io/v1
kind: DeviceClass
metadata:
  name: rak4631.usb.jacaudi.dev
spec:
  selectors:
    - cel:
        expression: >-
          device.driver == "usb.jacaudi.dev" &&
          device.attributes["usb.jacaudi.dev"].vendor == "239a" &&
          device.attributes["usb.jacaudi.dev"].product in ["8029"]
---
apiVersion: resource.k8s.io/v1
kind: ResourceClaimTemplate
metadata:
  name: rak4631-lora-port
  namespace: selfhosted
spec:
  spec:
    devices:
      requests:
        - name: lora
          exactly:
            deviceClassName: rak4631.usb.jacaudi.dev
            allocationMode: ExactCount
            count: 1
            selectors:
              - cel:
                  expression: device.attributes["usb.jacaudi.dev"].port == "PNP0D10:02-v2-1"
```

There is deliberately no match-all DeviceClass: it would let any namespace claim any
published device. The chart creates no DeviceClass on its own; add one class per device family
you want to hand out, through the chart's `deviceClasses` values.

## ttys and aliases

On prepare, the driver maps the device into the container through a per-claim CDI spec:

- the USB node `/dev/bus/usb/BBB/DDD`;
- every tty under the device's interfaces (`ttyACM*` from `cdc_acm`, `ttyUSB*` from
  usb-serial drivers), interfaces ordered by `bInterfaceNumber` and ttys within an interface by
  numeric suffix, each injected at `/dev/<name>`;
- each tty again at a stable alias `/dev/dra/<request>/tty<k>`. `<request>` is the claim
  request name (the part before `/` for a prioritized-list subrequest); `<k>` counts from 0
  across every tty of every device allocated to that request. A RAK4631 claimed by a request
  named `lora` appears at `/dev/dra/lora/tty0`.

Device nodes carry only a host path, a container path and `rw` permissions. Major, minor,
UID, GID and file mode are left for the runtime to fill from the host node, which keeps
injection correct across re-enumeration.

Environment variables, per device:

| Variable | Value |
| --- | --- |
| `USB_DEVNODE` | The USB node, `/dev/bus/usb/BBB/DDD`. |
| `USB_SERIAL` | The serial; set for serial-identity devices only. |
| `USB_TTY` | The host path of the device's first tty, when it has one. |

A claim holding two devices from this driver gets their `USB_*` variables merged per key.
Kubernetes `$(VAR)` expansion does not see CDI environment variables, which is why the
alias path exists: point your application at `/dev/dra/<request>/tty0`.

A device with no bound tty is injected with its USB node only; prepare does not fail.
`ttyUSB*` needs the usb-serial driver bound. On Talos 1.12 `cdc_acm` is built in; `ch341`,
`cp210x`, `ftdi_sio` and `pl2303` are modules that udev should autoload by modalias, which has
not been exercised on real hardware. If the tty does not appear, load the module via Talos
`machine.kernel.modules`.

## Prepared state

The driver records every prepared claim in `checkpoint.json` in its plugin data directory,
`/var/lib/kubelet/plugins/usb.jacaudi.dev/checkpoint.json`. It is written atomically (temp
file, sync, rename) after the claim's CDI spec on prepare and after the spec is removed on
unprepare, and loaded once at startup before the plugin registers, so prepared state survives
a driver restart.

Each device entry records the device name, the request, and the port. On prepare, the driver
resolves each allocated device by name from a fresh sysfs scan, then refuses any device that
another claim already holds in the checkpoint, matching on device name, or on port (so a device
that comes back under a new name on the same port is still caught) unless the holder's device
is attached on a different port now. Re-preparing a known claim
rewrites its CDI spec, which rebuilds the spec after a node reboot empties `/var/run/cdi`.
Allocation results with `adminAccess` are rejected, because two processes sharing a USB device
or tty corrupt each other. Results from a pool other than this node's pool are rejected too. Unprepare always removes the spec and checkpoint entry by claim
UID, and succeeds for an unknown claim.

A missing checkpoint is an empty state. An unreadable, unparseable or unknown-version file
stops the driver at startup with an error naming the file. To recover, delete the file and
restart the pod; the kubelet's own state still drives unprepare.

## Known limitations

- **Running containers keep their device nodes.** runc creates nodes by major:minor, and tty
  minors are reused lowest-free. If a board re-enumerates while its container runs, that
  container's `/dev/ttyACM0` can end up pointing at another board. The driver re-resolves only
  at prepare. The upstream signals for this (resource health, device taints) are alpha and
  disabled on the 1.35 cluster.
- **Container restarts reuse the prepared spec.** The kubelet does not re-prepare for a
  container restart, so a restarted container gets the paths resolved at the last prepare.
- **Port-identity swaps.** Two identical devices without usable serials swap identities if
  they swap ports.
- **Moved while claimed.** A port-identity device moved to another port while claimed is a new
  device to the driver and can be prepared for a second claim.
- **tty alias collisions.** Two claims in one container that reuse a request name collide on
  `/dev/dra/<request>/`; the runtime applies the last one without error. Use distinct request
  names.
- **Env merging.** A claim holding two devices from this driver gets their `USB_*` variables
  merged per key.

## Install

```sh
helm install usb-dra oci://ghcr.io/jacaudi/charts/usb-dra -n kube-system -f examples/values.yaml
kubectl apply -f examples/claims.yaml
```

Run it from a checkout of this repository, or copy `examples/values.yaml` and point `-f` at the
copy. A plain `helm install` without `-f` deploys the driver but **creates no DeviceClasses**: the
chart does not decide which devices to hand out, and a claim cannot select a device until a
class exists. `examples/values.yaml` defines `rtl-sdr` and `rak4631`; see
[Adding a DeviceClass](#adding-a-deviceclass).

The chart version equals the app version without the `v`: pin one with the bare semver, for
example `--version 0.1.0`. Check that the driver is publishing:

```sh
kubectl get resourceslices
kubectl get deviceclasses
```

Each node should show one `usb.jacaudi.dev` pool, empty when the node has no eligible devices.

`examples/claims.yaml` is an example: it targets the namespace `selfhosted`, so edit it to match
yours. It is not part of the chart.

Do not also expose these devices through generic-device-plugin: both would allocate the same
board.

Requires Kubernetes 1.34 or later (`resource.k8s.io/v1`) with DRA enabled on the kubelet.

- **Install into `kube-system`.** The DaemonSet runs as root and privileged on every node: the
  plugin socket and CDI directory are root-owned on the host. A namespace that enforces the
  baseline or restricted Pod Security level (Talos does outside `kube-system`) rejects the pods
  while `helm install` still reports success.
- **One release per cluster.** The driver name, the plugin socket on each node and the
  DeviceClass names are cluster-wide singletons.
- **Admission policy.** RBAC cannot scope ResourceSlice writes by node, so the chart also
  creates a ValidatingAdmissionPolicy: the driver's service account may write only its own
  node's `usb.jacaudi.dev` slices. It reads the node from the pod-bound service account token,
  which Kubernetes 1.34 includes by default. The policy is built from the release name and
  namespace, the same source as the service account, so they cannot drift apart.

### Values

The chart's own `values.yaml` is empty (`{}`): every default below lives in the templates, so
`examples/values.yaml` lists only what a deployment chooses. A value that is set is used as
given, even when empty or zero.

| Value | Default | Meaning |
| --- | --- | --- |
| `image.repository` | `ghcr.io/jacaudi/usb-dra` | Image repository. |
| `image.tag` | `v` plus the chart's `appVersion` | Used verbatim when set. |
| `image.pullPolicy` | `IfNotPresent` | `Always`, `IfNotPresent` or `Never`. |
| `driver.excludeClasses` | not passed; the binary's `03,08,e0` applies | `--exclude-classes`, see [What it publishes](#what-it-publishes). |
| `driver.include` | none | One `--include` per entry, each `vid:pid` or `vid:pid:serial`. |
| `driver.rescanInterval` | not passed; the binary's `30s` applies | `--rescan-interval`, a Go duration. |
| `driver.verbosity` | `2` | `-v`, 0 to 10. |
| `driver.cdiRoot` | `/var/run/cdi` | `--cdi-root` and the host mount move together; change it when a node's containerd uses a different `cdi_spec_dirs`. |
| `deviceClasses` | none: no DeviceClass is created | Map of DeviceClasses, see below. |
| `resources` | 10m CPU / 32Mi memory request, 128Mi memory limit | Container resources. A set value replaces the whole default. |
| `nodeSelector` | none | Restrict the DaemonSet to some nodes. |
| `tolerations` | `- operator: Exists` | Every node by default: an empty pool means "healthy, no devices". A set value replaces it. |
| `priorityClassName` | `system-node-critical` | Pod priority class. |

The chart builds the DaemonSet from fixed templates and reads only the values above, so a
values edit cannot drop the things the driver cannot run without: root, `privileged`, the host
mounts, the RBAC, the driver name `usb.jacaudi.dev`. Object names are the release name. The
values schema rejects unknown keys, name overrides and malformed IDs at render time instead of
ignoring them.

### Adding a DeviceClass

A device type is a values entry, not a chart release. Start from `examples/values.yaml`, which
defines `rtl-sdr` and `rak4631`. The key becomes the class name `<key>.usb.jacaudi.dev`;
`vendor` and each product are four lowercase hex digits and **must be quoted**, because an
unquoted `1e03` or `0403` is a YAML number and the schema rejects it.

```yaml
# my-values.yaml, layered over the example: -f examples/values.yaml -f my-values.yaml
deviceClasses:
  ch340: # DeviceClass ch340.usb.jacaudi.dev
    vendor: "1a86"
    products:
      - "7523"
  rak4631: null # drop a class the example defines
```

```sh
helm upgrade --install usb-dra oci://ghcr.io/jacaudi/charts/usb-dra -n kube-system \
  -f examples/values.yaml -f my-values.yaml
```

Each class selects this driver's devices of that vendor and one of those products, with the
`device.driver` guard every shipped selector needs.

### Without Helm

Every release also publishes `install.yaml` on its GitHub Release: the chart rendered with
`examples/values.yaml` (so it includes the `rtl-sdr` and `rak4631` DeviceClasses), release name
`usb-dra`, namespace `kube-system` and the release's image tag.

```sh
kubectl apply -f https://github.com/jacaudi/usb-dra/releases/download/v0.1.0/install.yaml
```

The release URL and `task artifact VERSION=` take the `v`-prefixed tag (`v0.1.0`), unlike the
chart's bare `--version 0.1.0`. Use Helm for any value other than the example's. Render it
yourself with `task artifact VERSION=v0.1.0`.

### Uninstall

`helm uninstall usb-dra -n kube-system` removes the DaemonSet, RBAC, admission policy and
DeviceClasses. The driver's ResourceSlices outlive it; delete them so the scheduler stops
seeing devices nothing serves:

```sh
kubectl delete resourceslices --field-selector spec.driver=usb.jacaudi.dev
```

## Upgrading from rtlsdr-dra

Installing the chart does not remove the old driver's objects. Delete them by hand:

- the `rtlsdr-dra` DaemonSet, ServiceAccount, ClusterRole and ClusterRoleBinding;
- the `sdr.jacaudi.dev` ResourceSlices;
- the old DeviceClass `rtl-sdr.jacaudi.dev`;
- `/var/lib/kubelet/plugins/sdr.jacaudi.dev` on each node.

Claims written against `sdr.jacaudi.dev` attributes or the old class must be rewritten for the
`usb.jacaudi.dev` attributes and the `rtl-sdr.usb.jacaudi.dev` class.

The injected environment variables were renamed: `RTLSDR_SERIAL` and `RTLSDR_DEVNODE` are now
`USB_SERIAL` and `USB_DEVNODE`; update any container that reads them.

## Upgrading from the manifests to the chart

Earlier builds installed from hand-written DaemonSet and DeviceClass manifests under
`deploy/`. The chart uses the same object names but different pod selector labels, and a
DaemonSet selector cannot be changed in place, so **delete the old DaemonSet first**. Helm
also refuses to take over objects it did not create, so delete the rest of what those
manifests created too, then install:

```sh
kubectl -n kube-system delete daemonset,serviceaccount usb-dra
kubectl delete clusterrole,clusterrolebinding usb-dra
kubectl delete validatingadmissionpolicy,validatingadmissionpolicybinding usb-dra-resourceslices
kubectl delete deviceclass rtl-sdr.usb.jacaudi.dev rak4631.usb.jacaudi.dev
helm install usb-dra oci://ghcr.io/jacaudi/charts/usb-dra -n kube-system -f examples/values.yaml
```

The same delete list applies to an install made from `install.yaml`. Those objects carry
`app.kubernetes.io/managed-by: Helm` but no `meta.helm.sh/release-*` annotations, so
`helm install` refuses to adopt them.

## Development

The Docker build does not run tests. Run `task test` locally or in CI; it needs Helm on the
`PATH`, because the chart tests render the chart.
