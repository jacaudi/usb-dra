# usb-dra Helm chart — design

Date: 2026-11-16. Size: small change (one chart, CI wiring, release-please package).

## Intent

Install usb-dra with Helm, from an OCI chart, instead of `kubectl apply -f deploy/`. The chart
must reproduce today's manifests exactly in behaviour, and must not let a values edit break the
deployment gotchas that were only found on a live cluster.

## Goals

- `helm install usb-dra oci://ghcr.io/jacaudi/charts/usb-dra` deploys a working driver.
- DeviceClasses are a values list, so a new device type is a values edit, not a chart release.
- The chart is versioned and published by the CI standard (release-please, `ci-chart.yml`).

## Non-goals

- Example claims (`deploy/claims.yaml`): per-app, namespace-specific; they stay examples.
- Metrics, Services, ServiceMonitor: the driver exposes none.
- Configurable driver name: the code, DeviceClasses and policy all assume `usb.jacaudi.dev`.

## Anti-goals

- A values edit that silently produces a pod which starts but cannot work: non-root, a missing
  host mount, or a disabled/mismatched admission policy.
- Two hand-maintained copies of the same RBAC, policy and DeviceClass knowledge.

## Constraints

- Kubernetes 1.34+ (`resource.k8s.io/v1`, `admissionregistration.k8s.io/v1` VAP).
- Root (`runAsUser: 0`) and `privileged: true` — CLAUDE.md deployment gotchas.
- The policy's `matchConditions` must name the service account the chart actually creates:
  `system:serviceaccount:<release namespace>:<service account name>`.
- CI standard: chart at `charts/`, `Chart.lock` committed, `.helmignore` beside `Chart.yaml`,
  `appVersion` maintained by the root release-please package via `extra-files` and an
  `# x-release-please-version` annotation; chart version maintained by its own package.

## Approaches

1. **Curated values on bjw-s common (picked by the user).** Short `values.yaml` with only the
   knobs this driver needs; small `_values-*.tpl` builders assemble common's structure and
   `common.yaml` hands it to the loader. Same pattern as `jacaudi/nextdns-operator`.
   Load-bearing settings live in templates, not values.
2. Pass values straight through to common. Less chart code, every common option available, but
   ~80 lines of values for the same DaemonSet and nothing stops a user dropping `privileged` or a
   mount. Rejected: violates the anti-goal.
3. Wrap app-template (as `jacaudi/dras` does). Not what was asked; same exposure as 2.

## Design

### Layout

```text
charts/
├── Chart.yaml            # name usb-dra; dependency common 5.3.0 (bjw-s-labs); appVersion annotated
├── Chart.lock
├── .helmignore
├── values.yaml
└── templates/
    ├── common.yaml               # builds the values dict, calls bjw-s.common.loader.all
    ├── _helpers.tpl              # fullname/name, service account name, driver name constant
    ├── _values-controllers.tpl   # the DaemonSet
    ├── _values-persistence.tpl   # the four hostPath mounts
    ├── _values-rbac.tpl          # ClusterRole + ClusterRoleBinding
    ├── _values-serviceaccount.tpl
    ├── validatingadmissionpolicy.yaml   # plain template: policy + binding
    └── deviceclasses.yaml               # plain template: range over .Values.deviceClasses
```

### values.yaml (the whole user surface)

```yaml
image:
  repository: ghcr.io/jacaudi/usb-dra
  tag: ""                 # empty -> Chart.appVersion
  pullPolicy: IfNotPresent
driver:
  excludeClasses: "03,08,e0"
  include: []             # each "vid:pid" or "vid:pid:serial" -> one --include
  rescanInterval: 30s
  verbosity: 2
deviceClasses:            # key -> DeviceClass "<key>.usb.jacaudi.dev"; null removes a default
  rtl-sdr:
    vendor: "0bda"
    products: ["2838", "2832"]
  rak4631:
    vendor: "239a"
    products: ["8029"]
serviceAccount:
  name: ""                # empty -> release fullname
resources:
  requests: { cpu: 10m, memory: 32Mi }
  limits: { memory: 128Mi }
nodeSelector: {}
tolerations:
  - operator: Exists      # every node: an empty pool means "healthy, no devices"
priorityClassName: system-node-critical
```

### Fixed in templates (not values)

- `privileged: true`, `runAsUser: 0`, `automountServiceAccountToken: true`.
- Mounts: `/var/lib/kubelet/plugins_registry`, `/var/lib/kubelet/plugins` (Directory),
  `/var/run/cdi` (DirectoryOrCreate), `/sys` (Directory, read-only).
- `NODE_NAME` from `spec.nodeName`; `--driver-name=usb.jacaudi.dev`.
- RBAC rules exactly as `deploy/daemonset.yaml` today (resourceslices rw, resourceclaims read,
  nodes get).
- The ValidatingAdmissionPolicy and binding, with the service account username built from
  `.Release.Namespace` and the service account name helper. Policy name is release-scoped so two
  releases cannot collide.

### DeviceClass rendering

Each entry renders:

```text
device.driver == "usb.jacaudi.dev" &&
device.attributes["usb.jacaudi.dev"].vendor == "<vendor>" &&
device.attributes["usb.jacaudi.dev"].product in [<products>]
```

`vendor` and each product are validated as four lowercase hex digits (`fail` on anything else),
matching what the driver publishes.

### deploy/ becomes generated (builder's proposal, flagged for review)

`deploy/daemonset.yaml` and `deploy/deviceclasses.yaml` are rendered from the chart with
`helm template` (release `usb-dra`, namespace `kube-system`) by a task, committed, and checked in
CI for drift. Reasons: one source of truth for RBAC, policy and DeviceClasses (anti-goal 2); the
existing Go tests (`TestSlicePolicyScopesDriverAccount`, the CEL selector tests) keep reading
`deploy/` and so test the chart's real output; `kubectl apply -f deploy/` keeps working for
non-Helm installs. `deploy/claims.yaml` stays hand-written.

### Release and CI wiring

- `release-please-config.json`: add package `charts` (release-type `helm`, component `chart`,
  tag `chart-v<version>`); root package gains `extra-files: charts/Chart.yaml` so `appVersion`
  follows app releases. Manifest gains `"charts": "0.0.0"` → first chart release 0.1.0.
- `ci.yaml`: uncomment `release-chart` (publishes to `oci://ghcr.io/jacaudi/charts`).
- `task lint` already runs `helm lint` + `helm template` when a chart exists.

## Verification

- `helm lint`, `helm template` in `task ci`; deploy/ drift check.
- Go tests pass against regenerated `deploy/`.
- kind v1.34 install from the packaged chart: DaemonSet ready on both nodes, empty pools
  published, policy denies a cross-node write (repeat of the S1 probe).

## Left to the builder

Helper names, exact tpl split, the drift-check task name, and README/CLAUDE.md wording.
