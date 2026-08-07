# rtlsdr-dra

A Kubernetes Dynamic Resource Allocation (DRA) driver that exposes RTL-SDR dongles as
schedulable devices, the way a GPU is exposed.

## Goal

Let a pod ask for "a radio" without naming hardware, while still allowing a pod to pin
one specific dongle when it matters.

The classic device-plugin API cannot do both. It has no selector: a pod requests a
*count* of a resource *name*, and the kubelet chooses which device IDs to hand over.
The only way to pin a particular dongle there is to give it its own resource name
(`devic.es/rtlsdr-acars`, `devic.es/rtlsdr-vdl2`), which bakes every serial into
cluster config. `generic-device-plugin` cannot express "N interchangeable units, each
bound to one distinct dongle" at all — its USB matcher collects every matched path into
one group and hands all of them to every allocatable unit.

DRA solves it by publishing the serial as a device **attribute**, so one driver serves
both cases: an unpinned claim draws from the pool, and a CEL selector pins a radio.

## Outcome

Verified end-to-end on kind v1.34.0 against a synthetic sysfs tree and `mknod`-ed USB
device nodes: ResourceSlice publishing, unpinned allocation, CEL serial pinning, CDI
injection of only the allocated dongle, pool exhaustion, and release-on-delete.

Not yet exercised: real RTL-SDR hardware (dongles were simulated) and multi-node
scheduling.

## Invariants — do not break these

These are load-bearing design decisions, each with a test asserting it. If a change
makes one of these tests fail, the change is wrong, not the test.

- **Never publish the device node, bus, or devnum as a ResourceSlice attribute.** Bus
  and device numbers change on every USB re-enumeration. A claim written against them
  would silently select the wrong radio after a replug. Only stable identity (serial,
  vendor, product) is published.
- **Resolve the device node at prepare time by looking up the serial**, never from a
  path captured when the slice was published. The dongle may have re-enumerated in
  between.
- **A node with no dongles still publishes an empty pool.** "Pool with no devices"
  means the driver is healthy and has no radios; "no pool" means the driver is absent.
  Publishing nothing also leaves a stale slice when the last dongle is pulled.
- **Never pin `major`/`minor` in the CDI spec.** CDI's `fillMissingInfo()` stats the
  host path, which is what keeps injection correct across re-enumeration.
- **Skip dongles with no serial.** Without one there is no durable identity to select
  on, and a pinned claim would be unsatisfiable.
- Prepare and unprepare must both be **idempotent** — the kubelet retries after a
  restart.

## Deployment gotchas

Both were found only by running the driver on a live cluster; neither shows up in unit
tests or `kubectl apply --dry-run=server`.

- **The image must NOT use a `:nonroot` base.** A kubelet plugin creates its socket
  under `/var/lib/kubelet/plugins/<driver>` and writes CDI specs to `/var/run/cdi`,
  both root-owned on the host. `privileged: true` does *not* change the UID, so a
  nonroot image dies at startup with `mkdir ...: permission denied`. The DaemonSet also
  sets `runAsUser: 0` explicitly.
- **RBAC needs `resourceclaims` read.** The kubeletplugin helper fetches the
  ResourceClaim itself before calling `NodePrepareResources`. Without it, every pod
  hangs in `ContainerCreating` with a forbidden error, and the driver logs look healthy.

## Layout

- `internal/rtlsdr/usb.go` — sysfs discovery. Filters interface directories (names
  containing `:`), hubs, and serial-less devices. `usb_real_test.go` holds a verbatim
  capture from a real Dell R730 node; keep it as the regression fixture.
- `internal/rtlsdr/slice.go` — device → ResourceSlice attributes.
- `cmd/rtlsdr-dra-kubeletplugin/` — kubelet plugin, CDI spec generation, prepare state.
- `deploy/` — DaemonSet + RBAC, DeviceClass + example claim templates.

Device names are `rtlsdr-<lowercased serial>` and must remain valid DNS labels.

## Commands

```sh
go test ./...                  # 27 tests; all must pass
go vet ./...
docker build -t rtlsdr-dra .   # runs the tests inside the build
kubectl apply --dry-run=server -f deploy/   # schema + CEL validation
```

Pin `k8s.io/*` deps to the minor version of the target cluster (currently v0.34.x for
Kubernetes 1.34).

## Duplicate serials

RTL-SDR sticks ship with factory serial `00000001`. Two un-reprogrammed dongles on one
host make a pinned claim ambiguous. `Discover` returns them via its `duplicates` return
and the driver logs an error. Fix with `rtl_eeprom -s <serial>` followed by a
**physical replug** — the EEPROM is only re-read at power-up, so re-enumeration alone
is not enough.

## Git identity

This repo sets `user.name`/`user.email` locally to
`jacaudi <47005674+jacaudi@users.noreply.github.com>`. The global git identity is a
private address that GitHub rejects on push. Do not commit with the global identity,
and do not put a real name or email into commits, messages, or files.
