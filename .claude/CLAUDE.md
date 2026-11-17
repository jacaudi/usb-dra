# usb-dra

A Kubernetes Dynamic Resource Allocation (DRA) driver (`usb.jacaudi.dev`) that publishes the
USB devices on each node as schedulable devices, the way a GPU is exposed.

## Goal

Let a pod ask for "a radio" or "the LoRa board" without naming hardware, while still allowing
a pod to pin one specific device when it matters.

The classic device-plugin API cannot do both. It has no selector: a pod requests a
*count* of a resource *name*, and the kubelet chooses which device IDs to hand over.
The only way to pin a particular device there is to give it its own resource name
(`devic.es/rtlsdr-acars`, `devic.es/rtlsdr-vdl2`), which bakes every serial into
cluster config.

DRA solves it by publishing stable identity (serial, port, vendor, product) as device
**attributes**: one driver publishes every eligible USB device, a `DeviceClass` CEL selector
says what a device *is* (RTL-SDR, RAK4631), and a claim selector pins one device by serial or
by port. Serial devices also get their ttys injected, with a stable alias path.

## Outcome

The code is built and unit-tested. The Helm chart is verified on a two-node kind v1.34.0:
DaemonSet ready on both nodes, an empty pool published per node, both DeviceClasses present, the
admission policy denying a cross-node slice write from the worker's pod-bound token, the rendered
`release.yaml` accepted by a server dry run, and a clean `helm uninstall`. Not yet exercised: real
hardware, multi-node scheduling, and a deploy into the cluster (a home-ops follow-up).

## Invariants — do not break these

These are load-bearing design decisions, each with the tests asserting it. If a change
makes one of these tests fail, the change is wrong, not the test.

1. **Never publish the device node, bus number, devnum, tty names or tty aliases.** They
   change on every re-enumeration; a claim written against them would silently select the
   wrong device after a replug. Tests: `TestAttributesNeverExposeNodes`.
2. **Resolve devices at prepare time by device name from a fresh scan, on every Prepare,
   including for claims already in the checkpoint.** The device may have re-enumerated since
   it was published. Tests: `TestPrepareResolvesNewNodeForKnownClaim`.
3. **A node with no eligible devices publishes an empty pool.** "Pool with no devices"
   means the driver is healthy; "no pool" means the driver is absent. Tests:
   `TestNodeResourcesPublishesEmptyPool`.
4. **CDI device nodes carry no major, minor, UID, GID or file mode.** CDI fills them from the
   host node, which keeps injection correct across re-enumeration. Tests:
   `TestWriteClaimSpecWithTTYs`.
5. **Prepare and unprepare are idempotent; unprepare always removes the spec and checkpoint
   entry by claim UID.** The kubelet retries after a restart. Tests:
   `TestPrepareIsIdempotent`, `TestUnprepareUnknownClaimRemovesSpec`.
6. **Hubs and vhci devices are never published.** Tests: `TestScanTalos01HubsOnly`,
   `TestScanSkipsVhciSilently`.
7. **Devices without a usable serial get port identity and `identity=port`.** Tests:
   `TestMissingSerialIsPortIdentity`, `TestAllZeroSerialIsPortIdentity`.
8. **The published slice is valid by construction:** DNS-label names at most 63 characters,
   unique, at most 128 devices, every string attribute at most 64 bytes. The API server rejects
   an invalid slice whole and the helper only logs it. Tests: `TestNodeResourcesCapsAt128`,
   `TestLongNameIsTruncatedWithHash`, `TestAttributesBoundStrings`,
   `TestScanSkipsOverlongPort`, `TestScanSkipsMalformedIDs`, `TestPortCollisionSkipsBoth`.
9. **One unreadable device never fails the scan or another device's prepare.** Tests:
   `TestScanSkipsUnreadableInterface`,
   `TestPrepareOneUnreadableDeviceDoesNotAffectOtherClaim`.
10. **Shipped selectors guard `device.driver` and optional attributes.** The shipped selectors
    are the DeviceClasses the chart renders from `examples/values.yaml`
    (`charts/templates/deviceclasses.yaml`) and the claims in `examples/claims.yaml`. A reference to a missing attribute aborts allocation.
    Tests: `TestClassSelectorsNeverError`, `TestSelectorsSerialPinSkipsDuplicate`.
11. **Prepared state survives a driver restart:** the checkpoint is written atomically and
    loaded before registration. Tests: `TestCheckpointSurvivesDriverRestart`,
    `TestSaveFailureLeavesStateUnchanged`.
12. **A device prepared for one claim is never prepared for another, by device name or last
    recorded port; admin-access results are rejected.** Tests:
    `TestPrepareRejectsDeviceHeldByOtherClaim`, `TestPrepareRejectsSamePortUnderNewName`,
    `TestPrepareAfterPortSwap`, `TestPrepareRejectsAdminAccess`.

## Deployment gotchas

The first two were found only by running the driver on a live cluster; neither shows up in
unit tests or `kubectl apply --dry-run=server`. The chart now fixes both in its templates,
with a test each.

- **The image must NOT use a `:nonroot` base.** A kubelet plugin creates its socket
  under `/var/lib/kubelet/plugins/<driver>` and writes CDI specs to `/var/run/cdi`,
  both root-owned on the host. `privileged: true` does *not* change the UID, so a
  nonroot image dies at startup with `mkdir ...: permission denied`. The chart's DaemonSet
  (`charts/templates/_values-controllers.tpl`) sets `runAsUser: 0` and `privileged: true`
  in the template, not in values. Test: `TestChartDaemonSetFixedFields`.
- **RBAC needs `resourceclaims` read.** The kubeletplugin helper fetches the
  ResourceClaim itself before calling `NodePrepareResources`. Without it, every pod
  hangs in `ContainerCreating` with a forbidden error, and the driver logs look healthy.
  The rules are in `charts/templates/_values-rbac.tpl`. Test: `TestChartRBAC`.
- **Install into `kube-system`.** The pods are privileged and root. A namespace enforcing
  baseline Pod Security (Talos does outside `kube-system`) rejects them while `helm install`
  reports success; `charts/templates/NOTES.txt` warns when the namespace differs.
- **The admission policy must match the service account the chart creates.** RBAC cannot scope
  ResourceSlice writes by node, so `charts/templates/validatingadmissionpolicy.yaml` does. Its
  username and the service account, ClusterRole, binding and DaemonSet names all come from the
  one `usb-dra.name` helper (the release name); the schema rejects every name override.
  Test: `TestSlicePolicyScopesDriverAccount` (a release-name by namespace matrix).

## Layout

- `internal/usb/sysfs.go` — sysfs discovery. Skips interface directories (names containing
  `:`), hubs and vhci devices; computes controller, revision and port; reads interfaces and
  ttys, ordered numerically.
- `internal/usb/filter.go` — `--exclude-classes` and `--include` parsing and the publish
  decision.
- `internal/usb/identity.go` — serial vs port identity, device names, uniqueness.
- `internal/usb/usb.go` — `Discover` and the `Device` type.
- `internal/usb/slice.go` — device to ResourceSlice attributes, the 128-device cap, and the
  always-present pool.
- `internal/usb/selectors_test.go` — compiles every selector the chart renders from
  `examples/values.yaml` and every claim in `examples/claims.yaml` with the DRA CEL package and
  evaluates them the way the allocator does.
- `internal/usb/chart*_test.go` — run `helm template` (fails, never skips, when Helm is
  missing) and assert the DaemonSet, RBAC, policy, DeviceClasses and schema rejections.
  `chart_style_test.go` fails on any flow-style YAML (`{...}`, `[...]`) under `charts/` and
  `examples/`: write block style.
- `internal/usb/testdata/sysfs/` — captures from talos-01-cp and talos-05-wrk; keep them as
  regression fixtures.
- `cmd/usb-dra-kubeletplugin/main.go` — flags, startup, rescan loop.
- `cmd/usb-dra-kubeletplugin/driver.go` — prepare (resolve, overlap check, spec, checkpoint)
  and unprepare.
- `cmd/usb-dra-kubeletplugin/cdi.go` — CDI spec generation: USB node, ttys, aliases, env.
- `cmd/usb-dra-kubeletplugin/checkpoint.go` — versioned atomic prepared-claim checkpoint.
- `charts/` — the Helm chart (`usb-dra`, on bjw-s `common` 5.3.0): the DaemonSet, ServiceAccount
  and RBAC through `common` from the `_values-*.tpl` helpers, the node-scoped
  ValidatingAdmissionPolicy, and the DeviceClasses rendered from the `deviceClasses` values.
  `charts/values.yaml` is `{}`: every default lives in the templates (hasKey/dig, so a set
  empty or zero value is honoured), and a plain install creates no DeviceClass.
  `values.schema.json` rejects unknown keys and malformed hex IDs. The chart version equals the
  app version (one release-please package). `charts/charts/` is the vendored dependency and is
  gitignored.
- `examples/values.yaml` — the example deployment: the `rtl-sdr` and `rak4631` DeviceClasses,
  with the driver defaults shown commented. `task artifact` renders `release.yaml` with it.
- `examples/claims.yaml` — example claim templates; not part of the chart.

Device names are `usb-<vid>-<pid>-<id>` and must remain valid DNS labels of at most 63
characters.

## Commands

```sh
task test                      # go test ./...: 96 tests, needs helm on PATH (the chart tests render it)
task ci                        # everything CI runs, helm lint + template included
go vet ./...
helm dependency build charts   # vendors common into charts/charts/ (honours Chart.lock)
helm lint charts
helm lint charts -f examples/values.yaml
task artifact VERSION=v0.1.0   # renders release.yaml from examples/values.yaml (gitignored)
kubectl apply --dry-run=server -f release.yaml   # schema + CEL validation of the rendered chart
docker build -t usb-dra .      # does NOT run tests: .dockerignore strips testdata and *_test.go
```

`k8s.io/*` stays v0.34.x while the cluster runs 1.35; bumping is a separate change.

## Duplicate serials

Two devices with the same serial (RTL-SDR sticks ship with the factory serial `00000001`), or
serials equal after case-folding or sanitizing, cannot be told apart by serial. Every device
sharing the final name falls back to port identity (`identity=port`) and the driver logs an
error naming the serial. A claim pinned with `identity == "serial" && serial == "<serial>"`
then matches neither device instead of picking one. Any names that still collide after the
fallback are all skipped. Fix with `rtl_eeprom -s <serial>` followed by a **physical
replug** — the EEPROM is only re-read at power-up, so re-enumeration alone is not enough.

## Git identity

This repo sets `user.name`/`user.email` locally to
`jacaudi <47005674+jacaudi@users.noreply.github.com>`. The global git identity is a
private address that GitHub rejects on push. Do not commit with the global identity,
and do not put a real name or email into commits, messages, or files.
