# usb-dra Helm chart — design

Date: 2026-11-16. Size: small change (one chart, CI wiring, release wiring). Revision 2, after an
adversarial review (findings B1–B4, S1–S5, M1–M5 below are that review's IDs).

## Intent

Install usb-dra with Helm from an OCI chart. The chart replaces the hand-written install manifests,
reproduces their behaviour exactly, and must not let a values edit break the deployment gotchas
that were only found on a live cluster.

## Goals

- `helm install usb-dra oci://ghcr.io/jacaudi/charts/usb-dra -n kube-system` deploys a working
  driver.
- DeviceClasses are a values map, so a new device type is a values edit, not a chart release.
- Every app release publishes a chart of the same version (lockstep), plus a rendered manifest on
  the GitHub Release for `kubectl apply` users.

## Non-goals

- Example claims: `deploy/claims.yaml` stays a hand-written example, not part of the chart.
- Metrics, Services, ServiceMonitor: the driver exposes none.
- A configurable driver name: code, DeviceClasses and policy all assume `usb.jacaudi.dev`.
- More than one release per cluster (S4): the driver name, plugin socket and DeviceClass names are
  cluster or node singletons. Stated in README and NOTES.txt.

## Anti-goals

- A values edit that yields a pod which starts but cannot work: non-root, a missing host mount, or
  an admission policy that no longer matches the service account it guards (B1).
- Two hand-maintained copies of the RBAC, policy and DeviceClass knowledge.

## Constraints

- Kubernetes 1.34+. Install into `kube-system` (S3): Talos enforces baseline Pod Security
  everywhere else, and the privileged pods would be rejected while `helm install` reports success.
- Root (`runAsUser: 0`) and `privileged: true` (CLAUDE.md deployment gotchas).
- GHCR rejects GitHub App tokens (GitHub Support, community discussion 24636). Every registry job,
  chart included, pushes with `GITHUB_TOKEN`; only release-please gets the App secrets.
- CI standard: chart at `charts/`, `Chart.lock` committed, `.helmignore` beside `Chart.yaml`.

## Decisions (user)

1. Curated values on bjw-s common, nextdns-operator pattern (not values pass-through, not
   app-template).
2. DeviceClasses as a values map with `rtl-sdr` and `rak4631` as defaults.
3. Lockstep versioning: one release-please package; chart `version` = app version.
4. `deploy/daemonset.yaml` and `deploy/deviceclasses.yaml` are deleted; the chart is the source.

## Approaches considered

1. **Curated values on common (picked).** Load-bearing settings live in templates; users reach only
   the knobs below.
2. Pass values straight to common: less code, but ~80 lines of values and nothing stops dropping
   `privileged` or a mount. Violates anti-goal 1.
3. Wrap app-template (as `jacaudi/dras`): not what was asked; same exposure as 2.

## Design

### Layout

```text
charts/
├── Chart.yaml           # name usb-dra; dependency common 5.3.0, repository oci://ghcr.io/bjw-s-labs/helm;
│                        #   kubeVersion ">=1.34.0-0"; version and appVersion annotated x-release-please-version
├── Chart.lock
├── .helmignore
├── values.yaml
├── values.schema.json
└── templates/
    ├── NOTES.txt
    ├── _helpers.tpl                  # the single name helper (B1) and the image ref (B4)
    ├── common.yaml                   # builds a fresh values dict, calls bjw-s.common.loader.all
    ├── _values-controllers.tpl       # DaemonSet
    ├── _values-persistence.tpl       # host mounts
    ├── _values-rbac.tpl              # ClusterRole + ClusterRoleBinding
    ├── _values-serviceaccount.tpl
    ├── validatingadmissionpolicy.yaml
    └── deviceclasses.yaml
```

### values.yaml (the whole user surface)

```yaml
global: {}                # required by common's name helpers (S5); constrained by the schema
image:
  repository: ghcr.io/jacaudi/usb-dra
  tag: ""                 # empty -> "v" + appVersion (B4)
  pullPolicy: IfNotPresent
driver:
  excludeClasses: "03,08,e0"
  include: []             # each "vid:pid" or "vid:pid:serial" -> one --include
  rescanInterval: 30s
  verbosity: 2
  cdiRoot: /var/run/cdi   # --cdi-root AND the hostPath/mountPath move together (M3)
deviceClasses:            # key -> DeviceClass "<key>.usb.jacaudi.dev"; null removes a default
  rtl-sdr:
    vendor: "0bda"
    products: ["2838", "2832"]
  rak4631:
    vendor: "239a"
    products: ["8029"]
resources:
  requests: { cpu: 10m, memory: 32Mi }
  limits: { memory: 128Mi }
nodeSelector: {}
tolerations:
  - operator: Exists      # every node: an empty pool means "healthy, no devices"
priorityClassName: system-node-critical
```

There is no `serviceAccount.name` value (B1: no present consumer, and a second name source).

### Names: one source (B1)

- `_helpers.tpl` defines one name helper (release fullname). It is handed to common as
  `forceRename` for the service account, ClusterRole, ClusterRoleBinding and DaemonSet, and is
  used directly by the policy's `matchConditions`, the binding subject and the policy name.
- `values.schema.json` rejects top-level `fullnameOverride`/`nameOverride` and every `global` key
  except `labels` and `annotations` (in particular `global.fullnameOverride`, `global.nameOverride`,
  `global.alwaysAppendIdentifierToResourceName`).
- A render-matrix test (below) asserts the service account name, the binding subject, the pod's
  `serviceAccountName` and the policy's username agree across at least three release-name /
  namespace combinations.

### Fixed in templates (not values)

- `privileged: true`, `runAsUser: 0`, `automountServiceAccountToken: true` (common v5 defaults it to
  false); `enableServiceLinks`, `hostIPC`, `hostNetwork`, `hostPID` set explicitly to false (M2).
- Mounts: `/var/lib/kubelet/plugins_registry`, `/var/lib/kubelet/plugins` (Directory),
  `driver.cdiRoot` (DirectoryOrCreate), `/sys` (Directory, read-only).
- `NODE_NAME` from `spec.nodeName`; `--driver-name=usb.jacaudi.dev`.
- RBAC rules exactly as today (resourceslices rw, resourceclaims read, nodes get).
- The ValidatingAdmissionPolicy and binding (node-scoped slice writes).
- `common.yaml` builds a new values dict (nextdns-operator pattern) and never merges user values
  into it, so `privileged` and friends are unreachable (M2). It seeds `namespace: {create: false}`
  (common 5.3 otherwise fails to render) and pins the controller identifier `main` (it becomes the
  immutable selector label).

### Image reference (B4)

`image.tag` empty renders `v` + `appVersion` with any leading `v` stripped, matching the
`v`-prefixed tags `ci-build` pushes. `appVersion` is written bare by release-please.

### DeviceClass rendering and validation (S2)

Each entry renders `device.driver == "usb.jacaudi.dev" && vendor == "<v>" && product in [<p>...]`.
`values.schema.json`: `vendor` and each product are `type: string`, pattern `^[0-9a-f]{4}$` (this
rejects YAML-coerced numbers such as `1e03`); keys match a DNS-label pattern; entries allow `null`;
`additionalProperties: false` throughout, including `driver.*` (exclude-classes and include
patterns mirror the driver's own validation).

### Install order (M1)

Helm creates unknown kinds last, so on first install the DaemonSet can briefly run before the
policy exists. Accepted: the window is seconds on first install only, and uninstall removes the
policy last. Documented in the design, not engineered around.

### Release and CI wiring

- `release-please-config.json` (one package, lockstep): root `.` gains
  `extra-files: [{type: generic, path: charts/Chart.yaml}]`; `version` and `appVersion` in
  `Chart.yaml` carry `# x-release-please-version`. `initial-version: 0.1.0` already set (#19).
- `.github/workflows/ci-chart.yml`: copied from `jacaudi/nextdns-operator` (lockstep variant: `ref`
  input, Helm v4.3.1 pinned, appVersion == release guard), not the template.
- `ci.yaml`: `release-chart` from nextdns-operator's lockstep block — keyed on `release_created`,
  `version`/`ref` from `tag_name`, `chart-path: charts`, no App secrets.
- `ci.yaml`: `release-artifact` enabled; `task artifact` renders the chart
  (`helm template usb-dra charts -n kube-system`) into `usb-dra.yaml` for the GitHub Release.
- Setup action: install Helm (pinned v4.3.1) when the detector reports `chart=true`; today it
  never installs Helm.
- `.taskfiles/repo.yml`: the chart finders use `-not -path '*/charts/*'`, which hides `./charts/`
  itself (B2-c). Exclude vendored subcharts only (`*/charts/charts/*`), and make
  `repo:chart:deps` run `helm dependency build` before lint/template.
- `.yamllint.yml` stops ignoring `charts/` except `charts/templates/` and `charts/charts/` (M4).
  `.gitignore` adds `/charts/charts/`.

### Tests replacing the deploy/ readers

`TestSlicePolicyScopesDriverAccount` and the CEL selector tests read `deploy/*.yaml` today. They
read `helm template` output instead: the test runs `helm template` (fails, not skips, when Helm is
missing) for a matrix of release name × namespace, decodes every document strictly, and asserts:

- the B1 four-way name identity and the policy's driver pin;
- each rendered DeviceClass CEL compiles and selects the right fixture devices (existing
  selectors_test logic, fed from the render);
- `deploy/claims.yaml` still decodes and compiles against the rendered classes.

## Verification

- `task ci`: helm lint/template, schema rejection cases (`fullnameOverride`, `vendor: 1e03`,
  unknown keys), Go render-matrix tests.
- kind v1.34: install the packaged chart into `kube-system`; DaemonSet ready on both nodes; empty
  pools published; the policy denies a cross-node slice write from the worker's token (the same
  probe used to verify the admission policy on 2026-11-14).
- First release PR: chart `version` and `appVersion` both bump to 0.1.0; `release-chart` and
  `release-artifact` run.

## Docs

README Install section becomes `helm install … -n kube-system` plus the release-manifest
alternative; "Upgrading from rtlsdr-dra" notes the DaemonSet selector changes (delete the old
DaemonSet first). CLAUDE.md Layout and Commands point at `charts/`.

## Left to the builder

Helper names, exact tpl split, schema wording, test file layout, NOTES.txt wording.
