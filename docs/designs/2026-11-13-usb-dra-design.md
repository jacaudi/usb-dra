# usb-dra — generic USB DRA driver

Date: 2026-11-13. Status: revised after design gate pass 2 and the user's BL1 decision
(industry-practice revision, "a-lite").

## Intent

`rtlsdr-dra` exposes RTL-SDR dongles as DRA devices: a pod asks for "a radio" or pins one
by serial with a CEL selector, instead of hardcoding a per-serial device-plugin resource
(`devic.es/rtlsdr-acars`). The same problem exists for every other USB device in the
home-ops cluster. The immediate one is a RAKwireless RAK4631 LoRa board (`239a:8029`,
`cdc_acm` → `/dev/ttyACM0`) on talos-05-wrk, which today needs its own
`generic-device-plugin` entry pinned to its `/dev/serial/by-id` path.

This change generalizes the driver: publish every locally attached, non-excluded USB
device with stable identity attributes, let `DeviceClass` CEL selectors say what a device
*is*, and inject serial ttys as well as the USB device node.

## Goals

- One driver publishes all eligible USB devices on a node. No per-device driver config.
- A device type (RTL-SDR, RAK4631) is a `DeviceClass` with a CEL selector on vendor/product.
- A claim can pin one device by serial, or by physical port when the device has no usable
  serial.
- Serial devices (`cdc_acm` → `ttyACM*`, usb-serial drivers → `ttyUSB*`) get their tty
  nodes injected, plus a stable alias path, not only `/dev/bus/usb/BBB/DDD`.
- Devices nobody should claim by accident (HID, mass storage, wireless controllers) are
  excluded by default, with flags to override.

## Non-goals

- `/dev/net/tun` and other non-USB or shared devices. They stay in `generic-device-plugin`.
  Revisit when `DRAConsumableCapacity` reaches beta (it is disabled on the 1.35 cluster:
  `kubernetes_feature_enabled{name="DRAConsumableCapacity",stage="ALPHA"} 0`).
- Deploying the driver into home-ops or migrating acars off `devic.es/rtlsdr-*`. Follow-up;
  see Follow-up notes.
- USB/IP or remote devices. Devices behind a `vhci_hcd` controller are skipped.
- Sharing one device between claims.
- Bumping `k8s.io/*` from v0.34.x.

## Anti-goals

- A claim that silently gets a different physical device than the one it pinned, where the
  driver can prevent it at prepare time. Cases it cannot prevent are documented under Known
  limitations, not hidden.
- Per-device driver configuration (an allowlist file).
- A match-all `DeviceClass` shipped by default. It would let any namespace claim any
  published device.
- One bad device taking down the node's whole ResourceSlice or every prepare on the node.

## Constraints

- Kubernetes ≥ 1.34, `resource.k8s.io/v1`. Cluster runs v1.35.10. `k8s.io/*` stays v0.34.3.
- Go toolchain go1.26.11, `golang.org/x/net` v0.60.0, `google.golang.org/grpc` v1.83.2 (govulncheck clean; user request 2026-11-14). `k8s.io/*` is not bumped with them.
- `.markdownlint-cli2.yaml` disables MD013 (line length) repo-wide.
- API server validation of a ResourceSlice (release-1.35 `pkg/apis/resource/validation`):
  device names are DNS-1123 labels (≤ 63) and unique within the slice; at most 128 devices
  per slice; string attribute values ≤ 64 bytes; ≤ 32 attributes per device; attribute IDs
  ≤ 32 characters. A slice that fails validation is rejected whole, and the kubeletplugin
  helper only logs the failure (`PublishResources` returns nil). The driver must therefore
  emit a valid slice by construction.
- CEL selectors abort allocation on a reference to an attribute a device lacks
  ("no such key"), and class selectors are evaluated against devices of every driver.
- Talos nodes: no shell, sysfs read-only into the pod, kubelet plugin and CDI dirs root-owned
  (the existing root/`privileged` deployment stays).

## Approaches considered

| | What decides what is published | Picked |
| --- | --- | --- |
| **A. Publish all non-hub devices, class exclusions with overrides** | Driver publishes everything except hubs and default-excluded classes; `DeviceClass` CEL does typing | **Yes** |
| B. Allowlist in driver config | Only listed `vid:pid` published | No: per-device config in two places, the pain being replaced |
| C. New repo, keep rtlsdr-dra | Same as A or B | No: duplicates working, tested code; rtlsdr-dra is not deployed, so nothing to protect |

The user picked A, with default exclusions that can be overridden.

Prior art: `re8ch/service-backend` `platform/apps/r640-usb-dra/main.go` (design reference
only; the repo has no license, so no code is copied). Ideas taken: vendor/product/serial as
generic attributes, `usb-<vid>-<pid>-<id>` device names, a final-name uniqueness check
(made non-fatal here). Its gaps that this design avoids: fatal duplicates, published hubs,
no tty, device node captured at publish time.

## Decisions

### Names (renaming is free: nothing references the old names)

| Thing | Old | New |
| --- | --- | --- |
| Repo / Go module | `jacaudi/rtlsdr-dra` | `jacaudi/usb-dra` (`github.com/jacaudi/usb-dra`) |
| Binary / cmd dir | `rtlsdr-dra-kubeletplugin` | `usb-dra-kubeletplugin` |
| Discovery package | `internal/rtlsdr` | `internal/usb` |
| Driver name | `sdr.jacaudi.dev` | `usb.jacaudi.dev` |
| CDI kind | `k8s.sdr.jacaudi.dev/rtlsdr` | `k8s.usb.jacaudi.dev/usb` |
| Env prefix | `RTLSDR_` | `USB_` |
| Image | `ghcr.io/jacaudi/rtlsdr-dra:v0.1.0` | `ghcr.io/jacaudi/usb-dra:latest` (CI publishes `latest` from `main`; no release stage exists. The home-ops deploy follow-up pins by digest.) |
| Example DeviceClasses | `rtl-sdr.jacaudi.dev` | `rtl-sdr.usb.jacaudi.dev`, `rak4631.usb.jacaudi.dev` |

An "Upgrading from rtlsdr-dra" section in the README covers removing the old objects.

The GitHub repository rename (user decision 2026-11-13, option b) is done by the unattended
run with `gh repo rename usb-dra --repo jacaudi/rtlsdr-dra --yes`, at the start of the
planning phase, before the feature worktree is created, followed by
`git remote set-url origin git@github.com:jacaudi/usb-dra.git`. CI derives the image name
from `github.repository`, so the image becomes `ghcr.io/jacaudi/usb-dra`. The local
directory `~/Projects/github/rtlsdr-dra` keeps its name; renaming it is left to the user.

### Discovery

Walk `sys/bus/usb/devices` (relative, read through `fs.FS`). Failing to read that directory
is the only error that fails a scan. Every other read error affects one device only: the
device is skipped for this scan and logged, and the scan continues. Severe skips (a name
collision, a port over 64 bytes), duplicate serials and the dropped-device count log at
error; other skips log at `V(2)`, which the DaemonSet runs (`-v=2`).

Files: `internal/usb/sysfs.go`; the exclusion and include filter is `internal/usb/filter.go`;
flag parsing is `cmd/usb-dra-kubeletplugin/main.go`.

For each entry whose name has no `:` (interface dirs have one) and that has `idVendor`,
`idProduct`, `busnum`, `devnum`:

1. **Hubs are always skipped.** `bDeviceClass == "09"`. Covers root hubs (`usbN`) and
   external hubs. No flag overrides it.
2. **USB/IP is skipped.** The controller (below) starts with `vhci_hcd`.
3. **Interfaces** are the entries named `<device>:<config>.<n>`, each with
   `bInterfaceClass` and `bInterfaceNumber`. Both files are two hex digits (sysfs prints
   them with `%02x`; the directory name's `<n>` is decimal and is not used for ordering).
   A device with no interface entries (unconfigured, `authorized=0`) is skipped and logged.
   A device where any interface's `bInterfaceClass` or `bInterfaceNumber` cannot be read or
   parsed as hex is skipped for this scan and logged (mid-enumeration).
4. **Include override** first: a device matching any `--include` entry is published,
   whatever its interface classes.
5. **Exclusion**: otherwise the device is excluded when every interface class is in the
   excluded set. Exclusion uses interface classes only, never `bDeviceClass` (real devices
   report `00` or `ef` there; the RAK4631 reports `ef`).
6. Read `serial`, `manufacturer`, `product` when present.

Flags (new, validated at startup; an invalid value is a startup error):

- `--exclude-classes` — comma-separated two-lowercase-hex-digit classes, default
  `03,08,e0`. It replaces the default; an empty value publishes everything but hubs and
  vhci devices.
- `--include` — repeatable, `vid:pid` or `vid:pid:serial`, split with
  `strings.SplitN(v, ":", 3)` so a serial may contain `:`. vid/pid are four lowercase hex
  digits; serial is compared case-insensitively; an empty serial (`vid:pid:`) is an error.

Worked example, talos-05-wrk today:

| Device | Interface classes | Result |
| --- | --- | --- |
| RTL-SDR `0bda:2838` ×2 | `ff` | published |
| RAK4631 `239a:8029` | `02`, `0a` | published |
| MediaTek `0e8d:c616` | `e0` ×3 | excluded |
| 16 xHCI root hubs | device class `09` | skipped (hub) |
| 2 vhci root hubs | device class `09` | skipped (hub) |

### Controller, revision and port

- **Controller**: the contents of `sys/bus/usb/devices/usb<busnum>/serial`. The kernel sets
  a root hub's serial to the controller's bus name (`drivers/usb/core/hcd.c`). Verified
  live: talos-01-cp `0000:00:1d.0`/`0000:00:1a.0`, talos-04-wrk `0000:00:0d.0`/
  `0000:00:14.0`, talos-05-wrk `PNP0D10:00`…`:09` and `vhci_hcd.0`.
- **Revision**: from `sys/bus/usb/devices/usb<busnum>/speed`: `1.5` or `12` → `1`,
  `480` → `2`, `5000`, `10000` or `20000` → `3`. Anything else (the kernel can report
  `unknown`) skips the device and logs it. xHCI registers two root hubs (USB 2 and USB 3) on one
  controller with independent port numbers; on talos-04 `3-1` and `4-1` are different
  connectors. udev adds the revision for the same reason (systemd `udev-builtin-path_id.c`,
  issue 19406; talos-05 has both `…-usb-0:1` and `…-usbv2-0:1` by-path links).
- **Port chain**: the device dir name after `<busnum>-` (`1`, `1.4`).
- **Port** = `<controller>-v<rev>-<chain>`, e.g. `PNP0D10:02-v2-1`,
  `0000:00:14.0-v3-1`. The bus number is never part of it.

A SuperSpeed device that falls back to high speed changes revision and therefore port.

Files: `internal/usb/sysfs.go`.

### Identity and device names

Every device gets a candidate name from its identity, then names are made unique.

- **Usable serial**: non-empty, valid UTF-8, ≤ 64 bytes, no control characters, not all zeros (`^0+$`, e.g. the
  MediaTek's `000000000`).
- **Serial identity** (devices with a usable serial): `<id>` = the serial.
- **Port identity** (everything else): `<id>` = `p-<port>`.
- **Name**: `usb-<vid>-<pid>-<id>`, lowercased; every character outside `[a-z0-9-]` becomes
  `-`; runs of `-` collapse; leading and trailing `-` are trimmed. If longer than 63: the
  first 54 characters, `-`, and the first 8 hex digits of the SHA-256 of the sanitized,
  untruncated name.
- **Uniqueness on the final name**: if two or more serial-identity devices end up with the
  same final name (shared serial, or serials equal after case-folding or sanitizing), every
  one of them switches to port identity and the duplicate is logged as an error, naming the
  serial. If any final names still collide after that, every colliding device is skipped
  and logged as an error.
- **Order and cap**: devices are sorted by final name. If more than 128 remain, the first
  128 are published and the number dropped is logged as an error.

Examples: `usb-0bda-2838-35650102`, `usb-239a-8029-fd14948954cd1f4f`,
`usb-1a86-7523-p-0000-00-14-0-v2-1-4`.

Behavior to accept and document:

- Swapping two identical port-identity devices between ports swaps their identities with
  no error. The `identity` attribute exists so a claim author can see this.
- When a second device with the same serial is plugged in, both flip to port identity. A
  claim pinned with the shipped serial idiom (below) then becomes unsatisfiable rather than
  matching either device. An already prepared claim keeps its spec until its next prepare,
  which fails with "no longer attached".

Files: `internal/usb/identity.go` and `internal/usb/usb.go`.

### Attributes (driver-qualified, `device.attributes["usb.jacaudi.dev"].<key>`)

| Key | Type | When | Bound | Example |
| --- | --- | --- | --- | --- |
| `vendor` | string | always | 4 chars | `239a` |
| `product` | string | always | 4 chars | `8029` |
| `identity` | string | always | — | `serial` or `port` |
| `port` | string | always; a device whose port exceeds 64 bytes is skipped and logged | ≤ 64 bytes | `PNP0D10:02-v2-1` |
| `serial` | string | when non-empty, valid UTF-8, ≤ 64 bytes, free of control characters and not all zeros (duplicates included) | ≤ 64 bytes | `FD14948954CD1F4F` |
| `manufacturer` | string | when reported and valid UTF-8; truncated on a rune boundary to ≤ 64 bytes | ≤ 64 bytes | `RAKwireless` |
| `productName` | string | same as `manufacturer` | ≤ 64 bytes | `WisCore RAK4631 Board` |
| `interfaceClasses` | string | when ≤ 64 bytes; sorted unique, comma-joined | ≤ 64 bytes | `02,0a` |

`interfaceClasses` is for operator visibility and optional selectors:
`"02" in a.interfaceClasses.split(",")` compiles and runs in the DRA CEL environment
(`ext.Strings` v2 is in the Kubernetes base environment). Class `02` also matches
CDC-Ethernet NICs, not only ttys.

Never published: device node, bus number, devnum, tty names, tty aliases.

Files: `internal/usb/slice.go`.

### CEL selector rules (shipped manifests and README must follow them)

`a` below is shorthand for `device.attributes["usb.jacaudi.dev"]`; manifests and the README
spell it out in full (CEL has no such variable).

- Every DeviceClass selector starts with `device.driver == "usb.jacaudi.dev"`, joined to the rest with `&&`.
- Pin by serial: `a.identity == "serial" && a.serial == "<serial>"`.
- Pin by port: `a.port == "<port>"` (`port` is always present).
- `serial`, `manufacturer`, `productName` and `interfaceClasses` are referenced only behind
  `a.identity == "serial"` (for `serial`) or `has(a.<key>)`.

A unit test compiles every selector in `deploy/` with `k8s.io/dynamic-resource-allocation/cel`
(stays on v0.34.x) and evaluates them the way the allocator does: each DeviceClass selector
against every fixture device, and each claim selector only against devices its DeviceClass
admits. Fixture devices: a serial-identity device, a port-identity device with no `serial`,
a port-identity device that kept a duplicate `serial`, and a device of another driver.
Every evaluation must succeed without error and match exactly the expected devices; the
pinned-by-serial selector must not match the duplicate.

Files: `deploy/*.yaml` and `internal/usb/selectors_test.go`.

### Prepared state (checkpoint)

Follows the practice of the reference driver (`kubernetes-sigs/dra-example-driver`) and the
NVIDIA, AMD, TPU and DiamondLightSource USB/IP drivers. The kubeletplugin helper says "it is
the responsibility of the DRA driver to cache whatever additional information it might
need" and that the driver "might have been restarted in the meantime"
(`kubeletplugin/draplugin.go:102,107`, v0.34.3); kubelet never re-sends Prepare for a claim
it holds as prepared (`pkg/kubelet/cm/dra/manager.go:362-364`, release-1.35).

- **File**: `checkpoint.json` in the plugin data directory
  (`/var/lib/kubelet/plugins/usb.jacaudi.dev/`, already a hostPath).
- **Content**: `{"version": 1, "claims": {"<uid>": {"namespace": "…", "name": "…",
  "devices": [{"device": "<device name>", "request": "<result.Request>",
  "port": "<port at prepare>"}]}}}`, devices in allocation-result order.
- **Writes**: atomic, the reference driver's way: `os.CreateTemp` in the same directory,
  write, `Sync`, `Rename` over the file. Written after the claim's CDI spec on Prepare, and
  after the spec is removed on Unprepare.
- **Load**: once at startup, before the plugin registers. A missing file is an empty state.
  An unreadable or unparseable file, or an unknown `version`, is a startup error naming the
  file (my call: fail loud; the operator deletes the file to recover, and kubelet's own
  checkpoint still drives Unprepare).
- **Not used**: `k8s.io/kubernetes/pkg/kubelet/checkpointmanager`; importing
  `k8s.io/kubernetes` costs more than the 40 lines it saves.
- **Concurrency**: the helper serializes all gRPC calls by default
  (`draplugin.go:71`); the driver keeps that default and holds its lock across
  read → resolve → write. The rescan ticker only publishes; it never touches prepared
  state.

Files: `cmd/usb-dra-kubeletplugin/checkpoint.go`.

### Prepare

For each claim:

1. Reject any allocation result for this driver with `AdminAccess` set (two processes
   sharing a USB device or tty corrupt each other) or from a pool other than this node's.
2. Resolve every allocated device from one fresh scan, never from the checkpoint or
   publish-time state. A device not found fails the claim with "no longer attached". This
   comes first because the overlap check needs each device's current port (ledger ruling B7).
3. **Overlap check** ("the last line of defense against using the same device for two
   different unrelated workloads", `draplugin.go:79-80`): if any allocated device is
   recorded in the checkpoint for a different claim UID, by device name or by last recorded
   port, fail with an error naming both claims and the device. A recorded port does not
   count when the holder's recorded device is attached on a different port now.
4. Assign the tty aliases and write the claim's CDI spec (below), whether or not the claim
   UID is already in the checkpoint. Re-preparing a known claim therefore picks up new node
   paths, and rebuilds the spec after a reboot wipes `/var/run/cdi` (tmpfs) while kubelet's
   checkpoint survives (kubernetes#141433; the reference driver does the same, PR #259).
5. Record the claim in the checkpoint and return the CDI IDs. CDI device names are scoped
   by claim UID and stay identical across re-prepares.

The claim's CDI spec contains, per resolved device:

- the USB node `/dev/bus/usb/BBB/DDD`;
- every tty under its interfaces, interfaces ordered by `bInterfaceNumber`:
  `<intf>/tty/<name>` (`cdc_acm`) and `<intf>/ttyUSB<n>` (usb-serial drivers), ttys within
  an interface in numeric suffix order; each injected at `/dev/<name>`;
- each tty again at the stable alias `/dev/dra/<request>/tty<k>` (user decision
  2026-11-13, option b). `<request>` is `resourceclaim.BaseRequestRef(result.Request)`, the
  part before `/` for a prioritized-list subrequest. `<k>` counts from 0 across every tty
  of every device allocated to that request, in allocation-result order, then the tty order
  above. A RAK4631 claimed by request `lora` appears at `/dev/dra/lora/tty0`.

Every device node is written with host path and container path (the alias's host path is
the real `/dev/<name>`) and `Permissions: "rw"`, and with no major, minor, UID, GID or file
mode: the runtime fills those from the host node and the container process.

Env, per device: `USB_DEVNODE`; `USB_SERIAL` (serial identity only); `USB_TTY` (host path
of the device's first tty, when it has one). Kubernetes `$(VAR)` expansion does not see CDI
env, which is why the alias exists.

**tty dependency.** `ttyUSB*` needs the usb-serial driver bound. On Talos 1.12
`CONFIG_USB_ACM=y`, and `ch341`, `cp210x`, `ftdi_sio`, `pl2303` are modules that udev
autoloads by modalias (`80-drivers.rules`; not exercised with real hardware). A device with
no bound tty is injected with its USB node only; prepare does not fail, and the README says
how to load the module through Talos machine config if autoload fails.

Files: `cmd/usb-dra-kubeletplugin/driver.go` and `cmd/usb-dra-kubeletplugin/cdi.go`.

### Unprepare

Remove the claim's CDI spec by claim UID, then its checkpoint entry, whether or not the
claim is known (`RemoveSpec` tolerates a missing file, CDI `cache.go:332-335`). Unknown
claims return success.

Files: `cmd/usb-dra-kubeletplugin/driver.go` and `cmd/usb-dra-kubeletplugin/cdi.go`.

### Known limitations (documented in the README)

- **Running containers keep their device nodes.** runc creates nodes by major:minor, and
  tty minors are reused lowest-free. If a board re-enumerates while its container runs,
  that container's `/dev/ttyACM0` can end up pointing at another board. The driver
  re-resolves only at prepare. The upstream signals for this, KEP-4680 resource health and
  KEP-5055 device taints, are alpha and disabled on the 1.35 cluster (beta targeted for
  1.36); adopting them is a follow-up.
- **Container restarts reuse the prepared spec.** kubelet does not re-prepare for a container
  restart, so a restarted container gets the paths resolved at the last prepare.
- **Port-identity swaps** (see Identity).
- **Moved while claimed**: a port-identity device moved to another port while claimed is a
  new device to the driver and can be prepared for a second claim.
- **tty alias collisions**: two claims in one container that reuse a request name collide on
  `/dev/dra/<request>/`; the runtime applies the last one without error. Use distinct
  request names.
- **Env merging**: a claim holding two devices from this driver gets their `USB_*` env merged
  per key.

### Shipped manifests

- `deploy/daemonset.yaml`: renamed driver, image `ghcr.io/jacaudi/usb-dra:latest`, flags at
  defaults.
- `deploy/deviceclasses.yaml`: `rtl-sdr.usb.jacaudi.dev`
  (`device.driver == "usb.jacaudi.dev" && a.vendor == "0bda" && a.product in ["2838", "2832"]`)
  and `rak4631.usb.jacaudi.dev` (`… && a.vendor == "239a" && a.product == "8029"`). No
  match-all class.
- `deploy/claims.yaml`: any RTL-SDR; RTL-SDR pinned by serial (serial idiom); RAK4631
  pinned by port (`a.port == "PNP0D10:02-v2-1"`), request named `lora`.

### Invariants (replace `.claude/CLAUDE.md`'s list; each needs a test asserting it)

1. Never publish the device node, bus number, devnum, tty names or tty aliases.
2. Resolve devices at prepare time by device name from a fresh scan, on every Prepare,
   including for claims already in the checkpoint.
3. A node with no eligible devices publishes an empty pool.
4. CDI device nodes carry no major, minor, UID, GID or file mode.
5. Prepare and unprepare are idempotent; unprepare always removes the spec and checkpoint
   entry by claim UID.
6. Hubs and vhci devices are never published.
7. Devices without a usable serial get port identity and `identity=port`.
8. The published slice is valid by construction: DNS-label names ≤ 63, unique, ≤ 128
   devices, every string attribute ≤ 64 bytes.
9. One unreadable device never fails the scan or another device's prepare.
10. Shipped selectors guard `device.driver` and optional attributes.
11. Prepared state survives a driver restart (checkpoint written atomically, loaded before
    registration).
12. A device prepared for one claim is never prepared for another, by device name or last
    recorded port; admin-access results are rejected.

Also update `.claude/CLAUDE.md`'s pin rule: `k8s.io/*` stays v0.34.x while the cluster runs
1.35; bumping is a separate change.

## Left to the builder

- Test fixtures (my call): while writing the implementation plan, the orchestrator captures
  talos-05-wrk (RAK4631, two RTL-SDRs, MediaTek, root hubs including `usb<N>/serial` and
  `speed`, vhci) and talos-01-cp (three hubs) read-only with `talosctl list`/`read`, and
  checks them in. The R730 fixture is replaced: its dongles have moved to talos-05 and its
  interface dirs carry no attribute files, so it cannot be re-captured. Synthetic trees
  cover usb-serial (`ttyUSB*`), port identity, duplicate serials, an over-length serial,
  interface number `0a`, an unreadable interface, and speed `unknown`.
- Internal structure, logging wording, README rewrite, within the decisions above.

## Follow-up notes (home-ops deploy, out of scope)

- talos-01-cp and talos-03-cp set containerd `cdi_spec_dirs` to `/var/cdi/static` and
  `/var/cdi/dynamic`; the DaemonSet's `/var/run/cdi` must match per node.
- `generic-device-plugin` exposes the same RTL-SDRs and the RAK4631. Running both lets one
  board be allocated twice; remove the overlapping entries when migrating.

## Unverified (carried from the gates and research)

- `$(VAR)` expansion not seeing CDI env is reasoned from the kubelet/runtime split, not read
  in source.
- The containerd and Talos versions actually running, and talos-05's default
  `cdi_spec_dirs`.
- Whether Talos udev autoloads `ch341` and similar by modalias.
- How often the RAK4631 re-enumerates in practice, and whether real descriptors in this
  cluster exceed 64 bytes.
- GitHub's redirect from the old repo name (documented behavior, not fetched).
- CRI-O's CDI refresh behavior (only containerd read; it calls `cdi.Refresh()` before every
  injection).
- The Google TPU driver's checkpoint behavior (reported by research, repo not located).

## Verified facts (2026-11-13)

- Root hub `serial` equals the controller name on all three controller types (see Controller).
- talos-04 `0000:00:14.0` hosts usb3 (480 Mb/s) and usb4 (10 Gb/s); `3-1` and `4-1` are
  different connectors (peer links).
- The RAK4631 reports `bDeviceClass=ef`; its interfaces are `02` and `0a`; its tty is
  `5-1:1.0/tty/ttyACM0`. RTL-SDR interfaces are `ff`; the MediaTek's are `e0`.
- talos-01-cp's three hubs report class `09`.
- CDI v1.1.0 fills UID/GID from the container process (`container-edits.go:93-102`) and
  file mode from the host (`container-edits_unix.go:103-105`); containerd v2.1.4/v2.2.0
  vendor CDI v1.0.1 with the same logic and apply the user before CDI.
- runc creates parent directories for device nodes, so `/dev/dra/<request>/` works.
- `resource.k8s.io/v1` `Request` uses `<main>/<sub>` for subrequests; request names are DNS
  labels.
