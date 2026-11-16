# usb-dra Helm Chart Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `deploy/daemonset.yaml` and `deploy/deviceclasses.yaml` with an OCI Helm chart at `charts/`, built on bjw-s common 5.3.0, released in lockstep with the app.

**Architecture:** Curated `values.yaml` feeds small `_values-*.tpl` builders; `common.yaml` assembles a fresh values dict and calls `bjw-s.common.loader.all` (the `jacaudi/nextdns-operator` `chart/templates/` pattern — read it with `gh api repos/jacaudi/nextdns-operator/contents/chart/templates/<file> --jq .content | base64 -d`). The admission policy and DeviceClasses are plain templates. Go tests render the chart with `helm template` and assert on the output.

**Tech Stack:** Helm v4.3.1, bjw-s common 5.3.0 (`oci://ghcr.io/bjw-s-labs/helm`), JSON Schema (draft-07) for values, Go 1.27.4 tests in `internal/usb`, Task, GitHub Actions, release-please.

**Spec:** `docs/designs/2026-11-16-helm-chart-design.md`

## Global Constraints

- Chart name `usb-dra`, path `charts/`, `kubeVersion: ">=1.34.0-0"`, dependency `common` `5.3.0` from `oci://ghcr.io/bjw-s-labs/helm`; `Chart.lock` committed; `charts/charts/` gitignored.
- Driver name is the constant `usb.jacaudi.dev`; never a value.
- Container: `privileged: true`, `runAsUser: 0`; pod: `automountServiceAccountToken: true`, `enableServiceLinks/hostIPC/hostNetwork/hostPID: false`; none reachable from values.
- Mounts: `/var/lib/kubelet/plugins_registry` and `/var/lib/kubelet/plugins` (`Directory`), `driver.cdiRoot` default `/var/run/cdi` (`DirectoryOrCreate`, same path for host and container, and passed as `--cdi-root`), `/sys` (`Directory`, read-only).
- Args: `--driver-name=usb.jacaudi.dev`, `--rescan-interval=<driver.rescanInterval>`, `--exclude-classes=<driver.excludeClasses>`, one `--include=<x>` per `driver.include` entry, `--cdi-root=<driver.cdiRoot>`, `-v=<driver.verbosity>`; env `NODE_NAME` from `spec.nodeName`.
- RBAC rules exactly as in today's `deploy/daemonset.yaml` (resourceslices get/list/watch/create/update/patch/delete; resourceclaims get/list/watch; nodes get).
- Image: `image.tag` empty renders `v` + `appVersion` with any leading `v` stripped; non-empty is used verbatim.
- Every registry/release job uses `GITHUB_TOKEN`; only the `release` job in `ci.yaml` receives App secrets.
- Lockstep: one release-please package; `Chart.yaml` `version` and `appVersion` both carry `# x-release-please-version`.
- Commit with the repo-local identity, signed; end messages with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. Release name other than `usb-dra` and namespace other than `kube-system` — the policy must still match the service account the chart creates (pinned in Task 2's matrix test).
2. YAML-coerced hex IDs (`vendor: 1e03`, `products: [10e2]`, `vendor: 0403`) — rejected at render, never a silently wrong selector (Task 3 schema test).
3. Typo'd value keys (`driver.excludeClass`, `deviceClasses.x.product`) — rejected, not ignored (Task 1 and Task 3 schema tests).
4. A user nulling one default class (`deviceClasses.rak4631: null`) — that class disappears, the other renders unchanged (Task 3).
5. `appVersion` written with or without a `v`, and an explicit `image.tag` — image renders `ghcr.io/jacaudi/usb-dra:v0.1.0` for both appVersion forms and the explicit tag verbatim (Task 1).

---

### Task 1: Chart skeleton, DaemonSet/RBAC/ServiceAccount, tooling

**Files:**

- Create: `charts/Chart.yaml`, `charts/Chart.lock`, `charts/.helmignore` (copy `~/.claude/skills/github-actions-align/templates/ignore/helmignore`), `charts/values.yaml`, `charts/values.schema.json`, `charts/templates/_helpers.tpl`, `charts/templates/common.yaml`, `charts/templates/_values-controllers.tpl`, `charts/templates/_values-persistence.tpl`, `charts/templates/_values-rbac.tpl`, `charts/templates/_values-serviceaccount.tpl`
- Create: `internal/usb/chart_test.go`
- Modify: `.taskfiles/repo.yml` (both `find … -name Chart.yaml` lines: replace `-not -path '*/charts/*'` with `-not -path '*/charts/charts/*'`)
- Modify: `.github/actions/setup/action.yml` (new step after the detector: install Helm `v4.3.1` with `azure/setup-helm` pinned by SHA when `steps.d.outputs.chart == 'true'`, independent of `lint-tools`)
- Modify: `.gitignore` (below the managed end marker: `/charts/charts/`), `.yamllint.yml` (ignore `charts/templates/` and `charts/charts/` instead of `charts/`)

**Interfaces:**

- Produces: `renderChart(t *testing.T, release, namespace string, extra ...string) ([]unstructured.Unstructured, error)` in `internal/usb/chart_test.go` — runs `helm dependency build ../../charts` once per test binary (`sync.Once`), then `helm template <release> ../../charts -n <namespace> <extra...>`; returns the decoded documents, or the helm error (stderr included). Fails the test (not skip) if `helm` is not on PATH.
- Produces: `mustRender(t, release, namespace string, extra ...string) []unstructured.Unstructured` (fatal on error) and `findKind(t, docs, kind string) []unstructured.Unstructured`.
- Produces: template helper `usb-dra.name` (the release fullname: release name, truncated to 63, trailing `-` trimmed; no overrides) in `_helpers.tpl`.

- [ ] **Step 1: Write the failing tests**

`TestChartDaemonSetFixedFields`: `mustRender(t, "usb-dra", "kube-system")`; exactly one DaemonSet; assert container `securityContext.privileged == true`, `runAsUser == 0`; pod `automountServiceAccountToken == true`, `hostNetwork == false`; the four volumes with the paths/types from Global Constraints and `/sys` mounted read-only; args equal `["--driver-name=usb.jacaudi.dev", "--rescan-interval=30s", "--exclude-classes=03,08,e0", "--cdi-root=/var/run/cdi", "-v=2"]`; env `NODE_NAME` uses `fieldRef.fieldPath: spec.nodeName`; `priorityClassName == system-node-critical`; tolerations `[{operator: Exists}]`.

`TestChartArgsFromValues`: render with `--set driver.include={0bda:2838,239a:8029:abc}` and `--set driver.cdiRoot=/run/cdi`; args contain `--include=0bda:2838` and `--include=239a:8029:abc` in order, `--cdi-root=/run/cdi`, and the cdi volume's `hostPath.path` and `mountPath` are both `/run/cdi`.

`TestChartImageTag` (table): appVersion via `--set image.tag=` empty with Chart appVersion as committed → `ghcr.io/jacaudi/usb-dra:v<appVersion without v>`; render a temp copy of the chart with `appVersion: "v0.9.0"` → `:v0.9.0`; `--set image.tag=sha-abc` → `:sha-abc`.

`TestChartRBAC`: ClusterRole rules equal the three rules in Global Constraints; ClusterRoleBinding `roleRef.name` equals the ClusterRole name; ServiceAccount exists in the release namespace.

`TestChartSchemaRejects` (table, each `renderChart` must return an error): `--set driver.excludeClass=03`, `--set fullnameOverride=x`, `--set nameOverride=x`, `--set global.fullnameOverride=x`, `--set global.alwaysAppendIdentifierToResourceName=true`, `--set image.pullPolicy=Sometimes`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOTOOLCHAIN=go1.27.4 go test ./internal/usb/ -run 'TestChart' -v`
Expected: FAIL (`charts/Chart.yaml` missing → helm error).

- [ ] **Step 3: Implement the chart pieces listed under Files**

`Chart.yaml`: `version: 0.0.0 # x-release-please-version`, `appVersion: "0.0.0" # x-release-please-version`. `values.yaml` exactly as the spec's block minus `deviceClasses` (added in Task 3) — keep `global: {}`. `common.yaml` follows nextdns-operator's `common.yaml`: fresh dict seeded with `global` and `namespace: {create: false}`, controllers identifier `main` (`type: daemonset`), service account identifier `main`, every common object given `forceRename: {{ include "usb-dra.name" . }}`. `values.schema.json`: `additionalProperties: false` at every object level; `global` allows only `labels` and `annotations`; `image.pullPolicy` enum `Always|IfNotPresent|Never`; `driver.excludeClasses` pattern `^([0-9a-f]{2}(,[0-9a-f]{2})*)?$`; `driver.include` items pattern `^[0-9a-f]{4}:[0-9a-f]{4}(:.+)?$`; `driver.verbosity` integer 0–10; `driver.cdiRoot` pattern `^/`. Run `helm dependency build charts` to create `Chart.lock`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `GOTOOLCHAIN=go1.27.4 go test ./internal/usb/ -run 'TestChart' -v` → PASS.
Run: `GOTOOLCHAIN=go1.27.4 task ci` → exit 0 and the output shows `linting ./charts`.

- [ ] **Step 5: Commit** (`feat(chart): add usb-dra chart on bjw-s common with the DaemonSet and RBAC`)

### Task 2: Admission policy and the single name source

**Files:**

- Create: `charts/templates/validatingadmissionpolicy.yaml`
- Modify: `internal/usb/deploy_test.go` → rename to `internal/usb/chart_policy_test.go`; replace the `deploy/daemonset.yaml` reader with `mustRender`

**Interfaces:**

- Consumes: `usb-dra.name`, `mustRender`, `findKind` (Task 1).

- [ ] **Step 1: Write the failing test**

`TestSlicePolicyScopesDriverAccount` becomes a table over `{release, namespace}` = `{usb-dra, kube-system}`, `{dra, kube-system}`, `{usb-dra, drivers}`. For each: exactly one ValidatingAdmissionPolicy and one binding; binding `policyName` equals the policy name and `validationActions` contains `Deny`; `failurePolicy == Fail`; with `user := "system:serviceaccount:" + ns + ":" + saName` where `saName` is the rendered ServiceAccount's name: the policy has a matchCondition with expression exactly `request.userInfo.username == "<user>"`, the ClusterRoleBinding subject is `{ServiceAccount, saName, ns}`, the DaemonSet's `serviceAccountName == saName`; some validation expression contains `'usb.jacaudi.dev'`; validations still contain the node-name and `[object, oldObject]` checks from today's policy.

- [ ] **Step 2: Run** `GOTOOLCHAIN=go1.27.4 go test ./internal/usb/ -run TestSlicePolicyScopesDriverAccount -v` → FAIL (no policy rendered).

- [ ] **Step 3: Implement `validatingadmissionpolicy.yaml`**

Port today's policy and binding from `deploy/daemonset.yaml` (git show `HEAD:deploy/daemonset.yaml`), naming both `{{ include "usb-dra.name" . }}-resourceslices` and building the username from `.Release.Namespace` and `usb-dra.name`.

- [ ] **Step 4: Run** the same test → PASS; `GOTOOLCHAIN=go1.27.4 go test ./...` → PASS.

- [ ] **Step 5: Commit** (`feat(chart): render the node-scoped slice policy from the chart's own names`)

### Task 3: DeviceClasses, and remove the hand-written manifests

**Files:**

- Create: `charts/templates/deviceclasses.yaml`
- Modify: `charts/values.yaml` (add the spec's `deviceClasses` block), `charts/values.schema.json` (`deviceClasses`: `propertyNames` pattern `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`; each value `null` or object with `vendor` string `^[0-9a-f]{4}$`, `products` non-empty array of strings `^[0-9a-f]{4}$`, `additionalProperties: false`)
- Modify: `internal/usb/selectors_test.go` (`loadSelectors` reads DeviceClasses from `mustRender(t, "usb-dra", "kube-system")`; claims still from `../../deploy/claims.yaml`)
- Delete: `deploy/daemonset.yaml`, `deploy/deviceclasses.yaml`

**Interfaces:**

- Consumes: `mustRender`, `findKind` (Task 1).

- [ ] **Step 1: Write the failing tests**

`TestChartDeviceClasses`: default render has exactly `rtl-sdr.usb.jacaudi.dev` and `rak4631.usb.jacaudi.dev`; the rtl-sdr expression is exactly
`device.driver == "usb.jacaudi.dev" && device.attributes["usb.jacaudi.dev"].vendor == "0bda" && device.attributes["usb.jacaudi.dev"].product in ["2838", "2832"]`.
With `-f` a temp values file `deviceClasses: {rak4631: null, gps: {vendor: "1546", products: ["01a8"]}}`: classes are exactly `rtl-sdr…` and `gps.usb.jacaudi.dev`.

`TestChartDeviceClassSchemaRejects` (each via a temp `-f` values file, written verbatim so YAML coercion happens): `vendor: 1e03`, `products: [10e2]`, `vendor: 0403`, `vendor: "0BDA"`, `product: ["2838"]` (wrong key), key `Bad_Name`.

Existing `TestSelectorsCompile` and the selector match tests keep their assertions and now pass against rendered classes.

- [ ] **Step 2: Run** `GOTOOLCHAIN=go1.27.4 go test ./internal/usb/ -v` → FAIL (no DeviceClasses rendered).

- [ ] **Step 3: Implement `deviceclasses.yaml`** — `range $name, $c := .Values.deviceClasses`, skip nil entries, render the expression above with products joined as `"a", "b"`.

- [ ] **Step 4: Delete the two deploy files; run** `GOTOOLCHAIN=go1.27.4 go test ./...` → PASS and `git grep -n 'deploy/daemonset.yaml\|deploy/deviceclasses.yaml' -- ':!docs/'` → only README/CLAUDE.md hits (fixed in Task 5).

- [ ] **Step 5: Commit** (`feat(chart)!: render DeviceClasses from values and drop the hand-written manifests`)

### Task 4: Release wiring (lockstep chart, release manifest)

**Files:**

- Create: `.github/workflows/ci-chart.yml` (copy verbatim from `jacaudi/nextdns-operator` `.github/workflows/ci-chart.yml`), `.github/workflows/ci-artifact.yml` (copy `~/.claude/skills/github-actions-align/templates/workflows/ci-artifact.yml`; keep this repo's newer pins: login-action v4.8.0 `8aa5831fb8d48d8f76594c7ddde61ae58019f550`)
- Modify: `.github/workflows/ci.yaml` — replace the commented `release-chart` block with nextdns-operator's lockstep `release-chart` job (its `ci.yaml` lines 148–162; set `chart-path: charts`); uncomment `release-artifact` with `assets: release.yaml`, `verify-images: ghcr.io/${{ github.repository }}:${VERSION}`, and **no** `secrets:`; add both jobs to the `ci` gate's `needs` and to its success/skip assertions the same way `release-image` is handled.
- Modify: `release-please-config.json` (root package: `"extra-files": [{"type": "generic", "path": "charts/Chart.yaml"}]`)
- Modify: `taskfile.yml` (add `artifact` task: requires `VERSION`; runs `task repo:chart:deps` then `helm template usb-dra charts -n kube-system --set image.tag="${VERSION}" > release.yaml`; prepend a comment line `# usb-dra ${VERSION} — install into kube-system`)
- Modify: `.gitignore` (`/release.yaml`)

- [ ] **Step 1: Verify the gaps exist**: `task artifact VERSION=v0.0.0` → `task: Task "artifact" does not exist`; `actionlint` → passes (jobs still commented).

- [ ] **Step 2: Make the changes listed under Files.**

- [ ] **Step 3: Verify**
  - `actionlint` → exit 0; `GOTOOLCHAIN=go1.27.4 task repo:actions:local-refs` → `local uses: references ok`.
  - `task artifact VERSION=v0.1.0` → `release.yaml` exists, contains `image: ghcr.io/jacaudi/usb-dra:v0.1.0`, a DaemonSet, the policy and two DeviceClasses; `kubectl apply --dry-run=client -f release.yaml` → all objects `created (dry run)`.
  - `uvx --quiet check-jsonschema --schemafile https://raw.githubusercontent.com/googleapis/release-please/main/schemas/config.json release-please-config.json` → `ok`.
  - `grep -n 'x-release-please-version' charts/Chart.yaml` → two lines.

- [ ] **Step 4: Commit** (`ci: publish the chart and a release manifest in lockstep with the app`)

### Task 5: Docs, NOTES, and live verification

**Files:**

- Create: `charts/templates/NOTES.txt` (installed into `{{ .Release.Namespace }}`; warn when it is not `kube-system` that Pod Security may reject the privileged pods; one release per cluster; `kubectl get resourceslices` check)
- Modify: `README.md` (Install: `helm install usb-dra oci://ghcr.io/jacaudi/charts/usb-dra -n kube-system`, the values surface, adding a DeviceClass via values, the `release.yaml` alternative; Upgrading: delete the old DaemonSet first because the selector changed; remove references to `deploy/daemonset.yaml`/`deviceclasses.yaml`)
- Modify: `.claude/CLAUDE.md` (Layout: `charts/` replaces the two deploy files; Commands: `helm lint charts`, the test count from `go test ./...`; invariants that cite deploy files now cite the chart templates and their tests)

- [ ] **Step 1: Write NOTES.txt and the doc changes.**

- [ ] **Step 2: Verify docs**: `npx -y markdownlint-cli2@0.24.1 README.md .claude/CLAUDE.md` → 0 issues; `git grep -n 'deploy/daemonset\|deploy/deviceclasses'` → no hits outside `docs/`.

- [ ] **Step 3: Live verification on kind v1.34.0 (two nodes)**
  - `helm package charts -d /tmp/chart` then `kind load docker-image` a locally built `usb-dra:dev`; `helm install usb-dra /tmp/chart/usb-dra-*.tgz -n kube-system --set image.repository=usb-dra --set image.tag=dev --set image.pullPolicy=Never --wait`.
  - DaemonSet ready 2/2; `kubectl get resourceslices` shows one empty `usb.jacaudi.dev` pool per node; `kubectl get deviceclasses` shows the two classes.
  - Mint a pod-bound token for the chart's service account bound to a pod on the worker; with only that token: creating a slice for the control-plane node → `DENIED … may only write ResourceSlices for that node`; own-node create → allowed.
  - `helm uninstall usb-dra -n kube-system` → policy, DaemonSet and classes gone. Delete the kind cluster; restore the previous kube context.

- [ ] **Step 4: Run** `GOTOOLCHAIN=go1.27.4 task ci` → exit 0.

- [ ] **Step 5: Commit** (`docs: install usb-dra with the helm chart`)
