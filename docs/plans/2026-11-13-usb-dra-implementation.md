# usb-dra Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the RTL-SDR-only DRA driver into a generic USB DRA driver (`usb.jacaudi.dev`) that publishes every eligible local USB device, injects serial ttys with stable aliases, and keeps prepared state across restarts.

**Architecture:** `internal/usb` owns sysfs discovery, filtering, identity/naming and ResourceSlice attributes as pure functions over `fs.FS`. `cmd/usb-dra-kubeletplugin` owns the kubelet plugin: a versioned JSON checkpoint, prepare/unprepare with per-claim CDI specs, and flag wiring. Device typing lives in `deploy/` DeviceClass CEL selectors, tested with `k8s.io/dynamic-resource-allocation/cel`.

**Tech Stack:** Go 1.26, `k8s.io/*` v0.34.3, `tags.cncf.io/container-device-interface` v1.1.0, go-task, golangci-lint v2.

**Spec:** docs/designs/2026-11-13-usb-dra-design.md

> **For Claude:** REQUIRED EXECUTION WORKFLOW (follow in order):
> 1. `superpowers:using-git-worktrees` — the worktree already exists; verify it, never create a second
> 2. `superpowers:subagent-driven-development` — one builder per task (batched where SDD says so), a fresh critic per task
> 3. `superpowers:test-driven-development` — every builder
> 4. `superpowers:verification-before-completion` — every task
> 5. Per-task review is SDD's own `task-reviewer-prompt.md` (built in)
> 6. After all tasks: whole-branch review by `sr-eng-review` (automatic; see the feature-workflow skill, Phase 4)
> 7. `superpowers:finishing-a-development-branch` — executes the run contract
>
> Skills carry their own model and effort settings. Do not override them; the feature-workflow skill's routing table is the only place a model is chosen.

**Run contract:**
- Integration when done: (c) keep the branch as-is (user, 2026-11-13).
- Base branch: `main`.
- Remote and forge: `git@github.com:jacaudi/usb-dra.git`, `gh`.
- Repository owned by the user: yes (`jacaudi/`).
- PR/MR options: not applicable (integration is (c)).
- Direct push to the base branch: never.
- Branch name: `feat/usb-dra`; worktree `/Users/acaudill/Projects/github/rtlsdr-dra/.worktrees/feat/usb-dra`.
- Sanctioned test command: `task test` (runs `go test ./... -race`).
- Sanctioned environment: host toolchain (no `.devcontainer/`).
- New dependencies: allowed only with a present requirement, pinned, named in the report.
- Boundaries: 8 hours wall clock; dispatch cap `6 × tasks + 12`; 2 hours without a task completing (defaults, user 2026-11-13).
- Worktree after (a) merge: remove (not applicable under (c)).
- Already done before this plan (user-authorized): GitHub repo renamed `jacaudi/rtlsdr-dra` → `jacaudi/usb-dra`, remote URL updated; sysfs fixtures committed (`c6f0425`).
- Commits are signed. A signing failure is `PLAN: PAUSED — signing`; never `--no-gpg-sign`. Every commit carries `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Global Constraints

- `k8s.io/*` stays at v0.34.3; `tags.cncf.io/container-device-interface` stays at v1.1.0. The only new direct import allowed is `k8s.io/dynamic-resource-allocation/cel` (same module) and `sigs.k8s.io/yaml` v1.6.0 (already in `go.sum`), in test code only.
- Driver name `usb.jacaudi.dev`; module `github.com/jacaudi/usb-dra`; CDI vendor `k8s.usb.jacaudi.dev`, class `usb`; env prefix `USB_`.
- Never publish device node, bus number, devnum, tty names or tty aliases as attributes.
- CDI device nodes carry `Path`, `HostPath` and `Permissions: "rw"` only: no `Major`, `Minor`, `UID`, `GID`, `FileMode`.
- ResourceSlice validity: device names are DNS-1123 labels ≤ 63 and unique; ≤ 128 devices; every string attribute ≤ 64 bytes.
- New or changed code comments are single lines, used where Go convention needs them (package and exported-identifier doc comments).
- After every task: `task test` passes, `golangci-lint run` reports no issues, `go mod tidy -diff` prints nothing.
- Test fixtures live under `internal/usb/testdata/sysfs/` so the Docker build (`COPY internal/`) sees them.

## Review Focus

- A device that unplugs mid-scan (attribute files vanish between reads) must not fail the scan or another claim's prepare — pinned in Task 3 (`TestDiscoverSkipsUnreadableInterface`) and Task 6 (`TestPrepareOneMissingDeviceDoesNotAffectOtherClaim`).
- A claim pinned by serial when that serial becomes duplicated must stop matching instead of matching either stick — pinned in Task 4 (`TestSelectorsSerialPinSkipsDuplicate`).
- After a node reboot `/var/run/cdi` is empty while the checkpoint survives; re-prepare must rewrite the spec — pinned in Task 6 (`TestPrepareRewritesSpecForKnownClaim`).
- A corrupt or future-version checkpoint must stop the driver at startup, not silently drop state — pinned in Task 5 (`TestLoadCheckpointRejectsUnknownVersion`, `TestLoadCheckpointRejectsGarbage`).
- Interface numbers ≥ 10 (`0a`) and `ttyUSB10` must order numerically, or alias `tty<k>` numbering shifts — pinned in Task 3 (`TestDiscoverOrdersInterfacesAndTTYsNumerically`).

---

### Task 1: Rename to usb-dra

Kind: rewrite

**Files:**
- Modify: `go.mod`
- Modify: `internal/rtlsdr/usb.go` → `internal/usb/usb.go` (git mv; package `usb`)
- Modify: `internal/rtlsdr/slice.go` → `internal/usb/slice.go`
- Modify: `internal/rtlsdr/usb_test.go`, `usb_real_test.go`, `slice_test.go` → `internal/usb/`
- Modify: `cmd/rtlsdr-dra-kubeletplugin/*.go` → `cmd/usb-dra-kubeletplugin/` (git mv)
- Modify: `deploy/daemonset.yaml`, `Dockerfile`, `.dockerignore` (only if it names the old paths)
- Move: `testdata/sysfs/*.tsv` → `internal/usb/testdata/sysfs/`

**Interfaces:**
- Produces: package `github.com/jacaudi/usb-dra/internal/usb` with `const DriverName = "usb.jacaudi.dev"` and `const SysfsUSBDir = "sys/bus/usb/devices"`; everything else keeps its current name and behavior (discovery still RTL-SDR-only, `DeviceName()` still `rtlsdr-<serial>`).

- [ ] **Step 1: Change the expected values in existing tests**
  In the moved tests replace `sdr.jacaudi.dev` with `usb.jacaudi.dev`, `RTLSDR_SERIAL=`/`RTLSDR_DEVNODE=` with `USB_SERIAL=`/`USB_DEVNODE=`, the CDI kind `k8s.sdr.jacaudi.dev/rtlsdr` with `k8s.usb.jacaudi.dev/usb`, and imports with `github.com/jacaudi/usb-dra/internal/usb`.

- [ ] **Step 2: Run tests to verify they fail**
  Run: `go test ./... 2>&1 | tail -5`
  Expected: FAIL (build errors on the old module path, or assertion failures on driver name/env).

- [ ] **Step 3: Apply the rename**
  `go.mod` module `github.com/jacaudi/usb-dra`; package `usb`; `DriverName = "usb.jacaudi.dev"`; `cdiClass = "usb"`; env keys `USB_SERIAL`, `USB_DEVNODE`. `deploy/daemonset.yaml`: every `rtlsdr-dra` name → `usb-dra`, `--driver-name=usb.jacaudi.dev`, image `ghcr.io/jacaudi/usb-dra:latest`. `Dockerfile`: build and copy `/usb-dra-kubeletplugin` from `./cmd/usb-dra-kubeletplugin`, entrypoint `/usr/bin/usb-dra-kubeletplugin`. `git mv testdata/sysfs internal/usb/testdata/sysfs`.

- [ ] **Step 4: Verify**
  Run: `task test && golangci-lint run && go mod tidy -diff && grep -rnE 'rtlsdr-dra|sdr\.jacaudi\.dev|RTLSDR_|internal/rtlsdr' cmd internal deploy/daemonset.yaml Dockerfile go.mod .github taskfile.yml .taskfiles`
  Expected: tests `ok` for both packages, `No issues found`, no tidy diff, and the grep prints nothing (exit 1).

- [ ] **Step 5: Commit**
  `git add -A cmd internal deploy/daemonset.yaml Dockerfile go.mod go.sum testdata .dockerignore && git commit -m "refactor: rename to usb-dra and usb.jacaudi.dev"` (with the Co-Authored-By trailer).

---

### Task 2: Device filter and flag parsing

**Files:**
- Create: `internal/usb/filter.go`
- Test: `internal/usb/filter_test.go`

**Interfaces:**
- Produces:
  - `const DefaultExcludeClasses = "03,08,e0"`
  - `type Include struct{ Vendor, Product, Serial string }`
  - `type Filter struct{ ExcludeClasses map[string]bool; Includes []Include }`
  - `func ParseExcludeClasses(s string) (map[string]bool, error)`
  - `func ParseInclude(s string) (Include, error)`
  - `func (f Filter) Publish(vendor, product, serial string, interfaceClasses []string) bool`

- [ ] **Step 1: Write the failing tests**

```go
func TestParseExcludeClasses(t *testing.T) {
	got, err := ParseExcludeClasses(DefaultExcludeClasses)
	// err == nil; got == map[string]bool{"03": true, "08": true, "e0": true}
	got, err = ParseExcludeClasses("")
	// err == nil; len(got) == 0
	for _, bad := range []string{"3", "0g", "03,,08", "E0", "003"} {
		_, err = ParseExcludeClasses(bad)
		// err != nil for every bad value
	}
}

func TestParseInclude(t *testing.T) {
	// "239a:8029" → Include{"239a", "8029", ""}, nil
	// "0bda:2838:35650102" → Include{"0bda", "2838", "35650102"}, nil
	// "0403:6001:A:B:C" → Include{"0403", "6001", "A:B:C"}, nil   (SplitN 3)
	// errors: "239a", "239a:", "239A:8029", "239a:802", "239a:8029:" (empty serial)
}

func TestFilterPublish(t *testing.T) {
	f := Filter{ExcludeClasses: map[string]bool{"03": true, "08": true, "e0": true},
		Includes: []Include{{"046d", "c52b", ""}, {"1234", "5678", "abc"}}}
	// f.Publish("0e8d", "c616", "000000000", []string{"e0"}) == false   (every interface excluded)
	// f.Publish("239a", "8029", "X", []string{"02", "0a"}) == true
	// f.Publish("1a2b", "3c4d", "", []string{"03", "ff"}) == true      (not every interface excluded)
	// f.Publish("046d", "c52b", "", []string{"03"}) == true             (include by vid:pid)
	// f.Publish("1234", "5678", "ABC", []string{"03"}) == true          (include serial is case-insensitive)
	// f.Publish("1234", "5678", "other", []string{"03"}) == false
	// Filter{}.Publish("0e8d", "c616", "", []string{"e0"}) == true      (empty exclude set)
}
```

- [ ] **Step 2: Run to verify failure**
  Run: `go test ./internal/usb/ -run 'TestParse|TestFilter' 2>&1 | tail -3`
  Expected: FAIL, build error `undefined: ParseExcludeClasses` (the compile failure is the RED state; add no stubs).

- [ ] **Step 3: Implement the signatures above in `internal/usb/filter.go`**
  Class tokens match `^[0-9a-f]{2}$`; vid/pid match `^[0-9a-f]{4}$`. `Publish` returns true when an include matches (serial compared with `strings.EqualFold`, empty include serial matches any), otherwise false only when `interfaceClasses` is non-empty and every element is in `ExcludeClasses`.

- [ ] **Step 4: Verify**
  Run: `task test && golangci-lint run`
  Expected: all `ok`, `No issues found`.

- [ ] **Step 5: Commit** `feat(usb): add device class filter and include overrides`.

---

### Task 3: Generic discovery, identity and attributes

Kind: rewrite

**Files:**
- Modify: `internal/usb/usb.go` (rewrite: discovery)
- Create: `internal/usb/identity.go` (port, names, uniqueness)
- Modify: `internal/usb/slice.go` (attributes, cap)
- Modify: `internal/usb/usb_test.go`, `internal/usb/slice_test.go`
- Delete: `internal/usb/usb_real_test.go` (its capture cannot be re-taken; replaced by the TSV fixtures)
- Create: `internal/usb/fixture_test.go`, `internal/usb/identity_test.go`
- Modify: `cmd/usb-dra-kubeletplugin/driver.go` (call sites only), `cmd/usb-dra-kubeletplugin/main.go` (config gains `filter`), `cmd/usb-dra-kubeletplugin/driver_test.go` (fake sysfs helper adds interfaces and root hubs)

**Interfaces:**
- Consumes: `Filter`, `DefaultExcludeClasses`, `ParseExcludeClasses` (Task 2).
- Produces:
  - `type Identity string` with `IdentitySerial Identity = "serial"`, `IdentityPort Identity = "port"`
  - `type Interface struct{ Number int; Class string; TTYs []string }` (TTYs are kernel names like `ttyACM0`, numeric order)
  - `type Device struct{ Name string; Identity Identity; Vendor, Product, Serial, Port, Manufacturer, ProductName string; Interfaces []Interface; Bus, Dev int }` — `Serial` is the serial when non-empty, valid UTF-8, ≤ 64 bytes and not `^0+$` (kept for duplicates), else `""`; `Manufacturer`/`ProductName` are bounded per the spec
  - `func (d Device) DevNode() string` → `/dev/bus/usb/%03d/%03d`
  - `func (d Device) InterfaceClasses() []string` (sorted unique)
  - `func (d Device) TTYs() []string` (`/dev/<name>`, interface order then tty order)
  - `type Skip struct{ Dir, Reason string }`
  - `type Scan struct{ Devices []Device; DuplicateSerials []string; Skipped []Skip }` (Devices sorted by Name, uncapped)
  - `func Discover(fsys fs.FS, f Filter) (Scan, error)` — errors only when `sys/bus/usb/devices` cannot be read
  - `func FindByName(fsys fs.FS, f Filter, name string) (Device, error)` — error text contains `no longer attached`
  - `func (d Device) Attributes() map[resourceapi.QualifiedName]resourceapi.DeviceAttribute`
  - `const MaxDevicesPerSlice = 128`
  - `func NodeResources(nodeName string, devices []Device) (resourceslice.DriverResources, int)` — second value is the number dropped by the cap
- Removes: `FindBySerial`, the `supported` allowlist, `Device.DeviceName()`.

- [ ] **Step 1: Write the fixture loader** in `internal/usb/fixture_test.go`:
  `func loadFixture(t *testing.T, name string) fstest.MapFS` reads `testdata/sysfs/<name>.tsv`; each line is `path<TAB>content`; it stores `fsys[path] = &fstest.MapFile{Data: []byte(content + "\n")}`. Also `func withFile(fsys fstest.MapFS, path, content string)` and `func without(fsys fstest.MapFS, path string)` helpers for synthetic variants.

- [ ] **Step 2: Write the failing tests** (in `usb_test.go`, `identity_test.go`, `slice_test.go`)

```go
var defaultFilter = Filter{ExcludeClasses: map[string]bool{"03": true, "08": true, "e0": true}}

func TestDiscoverTalos05(t *testing.T) {
	scan, err := Discover(loadFixture(t, "talos-05-wrk"), defaultFilter)
	// err == nil
	// names == []string{"usb-0bda-2838-35650102", "usb-0bda-2838-93610062", "usb-239a-8029-fd14948954cd1f4f"}
	// RAK: Identity serial, Port "PNP0D10:02-v2-1", DevNode() "/dev/bus/usb/005/002",
	//      Interfaces == []Interface{{0, "02", []string{"ttyACM0"}}, {1, "0a", nil}}, TTYs() == []string{"/dev/ttyACM0"}
	// 35650102: Port "PNP0D10:05-v2-1", DevNode() "/dev/bus/usb/011/002"
	// 93610062: Port "PNP0D10:04-v2-1", InterfaceClasses() == []string{"ff"}
	// len(scan.DuplicateSerials) == 0
}

func TestDiscoverTalos05NoExclusions(t *testing.T) {
	scan, _ := Discover(loadFixture(t, "talos-05-wrk"), Filter{})
	// 4 devices; MediaTek: Name "usb-0e8d-c616-p-pnp0d10-08-v2-1", Identity port, Serial "", Port "PNP0D10:08-v2-1"
}

func TestDiscoverTalos01HubsOnly(t *testing.T) {
	scan, err := Discover(loadFixture(t, "talos-01-cp"), Filter{})
	// err == nil; len(scan.Devices) == 0   (usb1, usb2, 1-1, 2-1, 2-1.6 are all class 09)
}

func TestDiscoverSkipsVhci(t *testing.T)                    // device 17-1 under usb17 (serial "vhci_hcd.0") is absent from Devices, present in Skipped
func TestDiscoverSkipsUnconfigured(t *testing.T)            // device dir with no "<dev>:" interface dirs → Skipped
func TestDiscoverSkipsUnreadableInterface(t *testing.T)     // remove 5-1:1.1/bInterfaceClass → RAK Skipped, both RTL-SDRs still in Devices, err == nil
func TestDiscoverSkipsUnknownSpeed(t *testing.T)            // usb5/speed "unknown" → RAK Skipped
func TestDiscoverMissingRootIsError(t *testing.T)           // empty fstest.MapFS{} → err != nil
func TestDiscoverOrdersInterfacesAndTTYsNumerically(t *testing.T)
	// synthetic device with interfaces bInterfaceNumber "0a" and "02" (dirs X:1.10 and X:1.2),
	// and ttyUSB10 + ttyUSB2 under one interface → Interfaces[0].Number == 2, Interfaces[1].Number == 10,
	// TTYs() order puts ttyUSB2 before ttyUSB10
func TestDiscoverBadInterfaceNumberSkips(t *testing.T)      // bInterfaceNumber "zz" → device Skipped

func TestPortRevisionFromSpeed(t *testing.T)
	// root hub speed → revision: "1.5"→"1", "12"→"1", "480"→"2", "5000"/"10000"/"20000"→"3"
func TestDuplicateSerialsFallBackToPort(t *testing.T)
	// two 0bda:2838 devices with serial "00000001" on different ports → both Identity port,
	// both Serial "00000001", DuplicateSerials == []string{"00000001"}
func TestCaseFoldedSerialsCollide(t *testing.T)            // serials "AbC" and "abc", same vid:pid → both port identity
func TestPortCollisionSkipsBoth(t *testing.T)
	// usb3 and usb4 both serial "0000:00:14.0", speed "480"; serial-less devices 3-1 and 4-1 → both Skipped, neither in Devices
func TestAllZeroSerialIsPortIdentity(t *testing.T)         // serial "000000000" → Identity port, Serial ""
func TestOverlongSerialIsPortIdentity(t *testing.T)        // 65-byte serial → Identity port, Serial ""
func TestLongNameIsTruncatedWithHash(t *testing.T)
	// 60-char lowercase alnum serial s; full := "usb-0bda-2838-" + s;
	// want := full[:54] + "-" + hex(sha256(full))[:8]; Name == want; len(Name) == 63

func TestAttributesRAK(t *testing.T)
	// exactly these keys and values: vendor "239a", product "8029", identity "serial",
	// port "PNP0D10:02-v2-1", serial "FD14948954CD1F4F", manufacturer "RAKwireless",
	// productName "WisCore RAK4631 Board", interfaceClasses "02,0a"
func TestAttributesPortIdentityHasNoSerial(t *testing.T)   // MediaTek: no "serial" key, identity "port"
func TestAttributesBoundStrings(t *testing.T)
	// manufacturer of 70 ASCII bytes → 64 bytes; manufacturer "\xff\xfe" (invalid UTF-8) → key absent;
	// every string value in every fixture device ≤ 64 bytes
func TestAttributesNeverExposeNodes(t *testing.T)          // no key in {"devnode","bus","busnum","devnum","tty","ttys"}
func TestNodeResourcesCapsAt128(t *testing.T)              // 130 devices → one slice with 128 devices, dropped == 2
func TestNodeResourcesPublishesEmptyPool(t *testing.T)     // nil devices → pool named after the node with one empty slice, dropped == 0
func TestFindByName(t *testing.T)                          // found for "usb-239a-8029-fd14948954cd1f4f"; "usb-dead-beef-x" → error containing "no longer attached"
```

  Synthetic devices are built with `withFile` on top of `loadFixture(t, "talos-05-wrk")` or an empty `fstest.MapFS` that includes the needed `usb<N>/serial`, `usb<N>/speed` and `usb<N>/bDeviceClass` (`09`) files.

- [ ] **Step 3: Run to verify failure**
  Run: `go test ./internal/usb/ 2>&1 | tail -3`
  Expected: FAIL (build errors on the new types).

- [ ] **Step 4: Implement** the Interfaces above per the spec's Discovery, Controller/revision/port, Identity and device names, and Attributes sections. Notes the spec leaves open, decided here:
  - Interfaces of device `D` are entries with prefix `D + ":"`; `bInterfaceNumber` and `bInterfaceClass` are parsed with `strconv.ParseUint(v, 16, 8)` and stored as int and as the original two-digit lowercase string respectively.
  - ttys: names of the entries under `<intf>/tty/` plus entries of `<intf>` whose name starts with `ttyUSB`; ordered by the numeric suffix after the last letter run.
  - Revision mapping: `1.5`,`12`→`1`; `480`→`2`; `5000`,`10000`,`20000`→`3`; anything else → Skip with reason `unknown root hub speed`.
  - A device is skipped (never published) when `Filter.Publish` returns false; that is not a `Skip` entry.
  - Name sanitizing: lowercase; `[^a-z0-9-]` → `-`; collapse `-+`; trim `-`.
  - `driver.go` call sites: `lookup` uses `usb.FindByName(d.sysfs, d.filter, res.Device)`; `preparedDevice.Serial` is set only when `Identity == usb.IdentitySerial`; `publish` logs `DuplicateSerials` (error), `Skipped` (V(2) info), and the dropped count from `NodeResources` (error when > 0).
  - `config` gains `filter usb.Filter`; `run()` sets it from `usb.ParseExcludeClasses(usb.DefaultExcludeClasses)` (flags arrive in Task 6).
  - `driver_test.go`'s fake sysfs builder writes, per device, `bDeviceClass` `00`, one interface `<dir>:1.0` with `bInterfaceClass` `ff` and `bInterfaceNumber` `00`, and root hub `usb<bus>` with `serial` `0000:00:14.0`, `speed` `480`, `bDeviceClass` `09`; device names in those tests become `usb-0bda-2838-<serial>`.

- [ ] **Step 5: Verify**
  Run: `task test && golangci-lint run && go mod tidy -diff`
  Expected: all `ok`, `No issues found`, no diff.

- [ ] **Step 6: Commit** `feat(usb): publish every eligible usb device with stable identity`.

---

### Task 4: DeviceClasses, example claims and selector tests

Kind: rewrite

**Files:**
- Create: `deploy/deviceclasses.yaml`
- Modify: `deploy/claims.yaml` (rewrite)
- Modify: `Dockerfile` (add `COPY deploy/ deploy/` before `RUN go test`)
- Create: `internal/usb/selectors_test.go`

**Interfaces:**
- Consumes: `Device.Attributes()`, `DriverName` (Task 3).

- [ ] **Step 1: Write the failing tests** in `internal/usb/selectors_test.go`:
  `func loadSelectors(t *testing.T) (classes map[string]string, claims map[string]struct{ Class, Expr string })` decodes every YAML document in `../../deploy/deviceclasses.yaml` and `../../deploy/claims.yaml` with `sigs.k8s.io/yaml` into `resourceapi.DeviceClass` / `resourceapi.ResourceClaimTemplate`, collecting `spec.selectors[].cel.expression` and each request's `exactly.deviceClassName` and `exactly.selectors[].cel.expression` (a template with no selector gets `Expr == ""`). Compile with `cel.GetCompiler(cel.Features{}).CompileCELExpression(expr, cel.Options{})`; evaluate with `DeviceMatches(ctx, cel.Device{Driver: d, Attributes: attrs})`.
  Fixture devices: the talos-05 RAK and both RTL-SDRs from `Discover`; a port-identity `0bda:2838` device with `Serial` `"35650102"` (duplicate case); a port-identity device with no serial; a foreign device `cel.Device{Driver: "gpu.example.com", Attributes: {"model": "x"}}`.

```go
func TestSelectorsCompile(t *testing.T)              // every expression compiles (result.Error == nil)
func TestClassSelectorsNeverError(t *testing.T)      // every class expr against every fixture device: err == nil
func TestClassSelectorsMatch(t *testing.T)
	// rtl-sdr.usb.jacaudi.dev matches exactly the two RTL-SDRs and the duplicate-case device;
	// rak4631.usb.jacaudi.dev matches exactly the RAK
func TestClaimSelectorsMatch(t *testing.T)
	// each claim expr evaluated only on devices its class admits, err == nil:
	// rtl-sdr-any → no expr; rtl-sdr-35650102 → matches only the serial-identity 35650102;
	// rak4631-lora-port → matches only the RAK (port "PNP0D10:02-v2-1")
func TestSelectorsSerialPinSkipsDuplicate(t *testing.T) // the 35650102 pin does not match the port-identity duplicate
```

- [ ] **Step 2: Run to verify failure**
  Run: `go test ./internal/usb/ -run 'Selector' 2>&1 | tail -3`
  Expected: FAIL (`deploy/deviceclasses.yaml` missing).

- [ ] **Step 3: Write the manifests** per the spec's Shipped manifests and CEL selector rules, spelling out `device.attributes["usb.jacaudi.dev"]`:
  - `deploy/deviceclasses.yaml`: `rtl-sdr.usb.jacaudi.dev` with `device.driver == "usb.jacaudi.dev" && device.attributes["usb.jacaudi.dev"].vendor == "0bda" && device.attributes["usb.jacaudi.dev"].product in ["2838", "2832"]`; `rak4631.usb.jacaudi.dev` with the same driver guard, vendor `239a`, product `8029`.
  - `deploy/claims.yaml` (namespace `selfhosted`): `ResourceClaimTemplate` `rtl-sdr-any` (request `radio`, class `rtl-sdr.usb.jacaudi.dev`, no selector); `rtl-sdr-35650102` (request `radio`, selector `device.attributes["usb.jacaudi.dev"].identity == "serial" && device.attributes["usb.jacaudi.dev"].serial == "35650102"`); `rak4631-lora-port` (request `lora`, class `rak4631.usb.jacaudi.dev`, selector `device.attributes["usb.jacaudi.dev"].port == "PNP0D10:02-v2-1"`).
  - File-top comment: one line per file.

- [ ] **Step 4: Verify**
  Run: `task test && golangci-lint run && go mod tidy -diff && kubectl apply --dry-run=client -f deploy/deviceclasses.yaml -f deploy/claims.yaml`
  Expected: all `ok`, `No issues found`, no diff, and kubectl prints `created (dry run)` for 2 DeviceClasses and 3 ResourceClaimTemplates. (`go mod tidy` may promote `sigs.k8s.io/yaml` to a direct requirement; commit that change.)

- [ ] **Step 5: Commit** `feat(deploy): add usb deviceclasses and guarded example claims`.

---

### Task 5: Checkpoint file

**Files:**
- Create: `cmd/usb-dra-kubeletplugin/checkpoint.go`
- Test: `cmd/usb-dra-kubeletplugin/checkpoint_test.go`

**Interfaces:**
- Produces:
  - `const checkpointFile = "checkpoint.json"`, `const checkpointVersion = 1`
  - `type checkpointDevice struct{ Device string `json:"device"`; Request string `json:"request"` }`
  - `type checkpointClaim struct{ Namespace string `json:"namespace"`; Name string `json:"name"`; Devices []checkpointDevice `json:"devices"` }`
  - `type checkpoint struct{ Version int `json:"version"`; Claims map[string]checkpointClaim `json:"claims"` }`
  - `func loadCheckpoint(path string) (checkpoint, error)` — missing file → `checkpoint{Version: 1, Claims: {}}`, nil
  - `func (c checkpoint) save(path string) error` — atomic
  - `func (c checkpoint) owner(device string) (claimUID string, ok bool)`

- [ ] **Step 1: Write the failing tests**

```go
func TestLoadCheckpointMissingIsEmpty(t *testing.T)       // Version 1, len(Claims) == 0, err == nil
func TestCheckpointRoundTrip(t *testing.T)
	// save {Claims: {"uid-a": {"selfhosted", "lora", [{"usb-239a-8029-fd14948954cd1f4f", "lora"}]}}};
	// load → deep-equal; file content contains `"version": 1`
func TestCheckpointSaveIsAtomic(t *testing.T)
	// after save, the directory holds exactly checkpoint.json (no temp files left)
func TestLoadCheckpointRejectsUnknownVersion(t *testing.T) // file {"version": 2, "claims": {}} → err contains "version 2"
func TestLoadCheckpointRejectsGarbage(t *testing.T)        // file "not json" → err contains the file path
func TestCheckpointOwner(t *testing.T)                     // owner("usb-239a-8029-fd14948954cd1f4f") == ("uid-a", true); owner("x") == ("", false)
```

- [ ] **Step 2: Run to verify failure**
  Run: `go test ./cmd/usb-dra-kubeletplugin/ -run Checkpoint 2>&1 | tail -3`
  Expected: FAIL (`undefined: loadCheckpoint`).

- [ ] **Step 3: Implement** per the spec's Prepared state section: `encoding/json` with `MarshalIndent(…, "", "  ")`; save is `os.CreateTemp(dir, "checkpoint-*.tmp")`, write, `Sync`, `Close`, `os.Rename` onto `path`, removing the temp file on any error.

- [ ] **Step 4: Verify** — Run: `task test && golangci-lint run`; Expected: all `ok`, `No issues found`.

- [ ] **Step 5: Commit** `feat(kubeletplugin): add versioned atomic checkpoint`.

---

### Task 6: Prepare with ttys, aliases, checkpoint and overlap check

Kind: rewrite

**Files:**
- Modify: `cmd/usb-dra-kubeletplugin/cdi.go`
- Modify: `cmd/usb-dra-kubeletplugin/driver.go`
- Modify: `cmd/usb-dra-kubeletplugin/main.go`
- Modify: `cmd/usb-dra-kubeletplugin/cdi_test.go`, `cmd/usb-dra-kubeletplugin/driver_test.go`
- Modify: `deploy/daemonset.yaml` (args gain `--exclude-classes=03,08,e0`)

**Interfaces:**
- Consumes: `usb.FindByName`, `usb.Device.TTYs()`, `usb.Filter`, `usb.ParseExcludeClasses`, `usb.ParseInclude` (Tasks 2–3); `loadCheckpoint`, `checkpoint.save`, `checkpoint.owner` (Task 5).
- Produces:
  - `type preparedDevice struct{ DeviceName, DevNode, Serial string; TTYs, Aliases []string; Requests []string }` (`Aliases[i]` is the container path for host `TTYs[i]`)
  - `func newDriverCore(cfg config) (*driver, error)` — builds the CDI handler and loads the checkpoint from `filepath.Join(cfg.pluginDir, checkpointFile)`; `newDriver` calls it before `kubeletplugin.Start`
  - `func assignAliases(devices []preparedDevice)` — fills `Aliases` per the spec
  - `func parseFlags(args []string) (config, error)` — owns flags `--exclude-classes` (default `usb.DefaultExcludeClasses`) and repeatable `--include`, validating both; `run()` calls it before any client is built
- Removes: the in-memory `prepared` map.

- [ ] **Step 1: Write the failing tests**

```go
// cdi_test.go
func TestWriteClaimSpecWithTTYs(t *testing.T)
	// device {DeviceName "usb-239a-8029-fd14948954cd1f4f", DevNode "/dev/bus/usb/005/002",
	//   Serial "FD14948954CD1F4F", TTYs ["/dev/ttyACM0"], Aliases ["/dev/dra/lora/tty0"], Requests ["lora"]}
	// spec device nodes, in order: {Path "/dev/bus/usb/005/002", HostPath same}, {"/dev/ttyACM0", "/dev/ttyACM0"},
	//   {Path "/dev/dra/lora/tty0", HostPath "/dev/ttyACM0"}; every node Permissions "rw",
	//   Major == 0, Minor == 0, UID == nil, GID == nil, FileMode == nil
	// env == ["USB_DEVNODE=/dev/bus/usb/005/002", "USB_SERIAL=FD14948954CD1F4F", "USB_TTY=/dev/ttyACM0"]
func TestWriteClaimSpecPortIdentityNoSerialEnv(t *testing.T) // Serial "" and no TTYs → env == ["USB_DEVNODE=…"]
func TestAssignAliases(t *testing.T)
	// requests "lora" (device A: 1 tty), "lora/primary" (device B: 2 ttys), "radio" (device C: 0 ttys)
	// → A ["/dev/dra/lora/tty0"], B ["/dev/dra/lora/tty1", "/dev/dra/lora/tty2"], C nil

// driver_test.go (newTestDriver now calls newDriverCore with pluginDir and cdiRoot = t.TempDir())
func TestPrepareRAKInjectsTTY(t *testing.T)                 // talos-05 fixture, request "lora": spec contains /dev/ttyACM0 and /dev/dra/lora/tty0
func TestPrepareRejectsAdminAccess(t *testing.T)            // result.AdminAccess = ptr(true) → Err contains "admin access"
func TestPrepareRejectsDeviceHeldByOtherClaim(t *testing.T)
	// prepare uid-a with RAK; prepare uid-b with RAK → uid-b Err contains "uid-a", "uid-b" and "usb-239a-8029-fd14948954cd1f4f"
func TestPrepareRewritesSpecForKnownClaim(t *testing.T)
	// prepare uid-a; delete every file in cdiRoot; prepare uid-a again → one spec file exists; CDIDeviceIDs equal across both calls
func TestPrepareResolvesNewNodeForKnownClaim(t *testing.T)
	// prepare uid-a; set 5-1/devnum to "7"; prepare uid-a → spec contains "/dev/bus/usb/005/007"
func TestPrepareOneMissingDeviceDoesNotAffectOtherClaim(t *testing.T)
	// batch of uid-a (RAK) and uid-b (device "usb-dead-beef-x") → uid-a prepared, uid-b Err contains "no longer attached"
func TestCheckpointSurvivesDriverRestart(t *testing.T)
	// driver 1 prepares uid-a; driver 2 = newDriverCore on the same dirs; driver 2 rejects uid-b for the RAK;
	// driver 2 unprepares uid-a → no spec file, checkpoint has no "uid-a"
func TestUnprepareUnknownClaimRemovesSpec(t *testing.T)
	// write a spec for uid-z via cdi.writeClaimSpec only; unprepare uid-z → nil error, spec gone
func TestRunRejectsBadFlags(t *testing.T)
	// parseFlags-level check: "--exclude-classes=zz" and "--include=239a" each return an error before any client is built
```
  Keep the existing prepare/unprepare tests (idempotency, other-driver results ignored, unallocated claim rejected, no-devices-for-us rejected), renamed to the new device names.

- [ ] **Step 2: Run to verify failure**
  Run: `go test ./cmd/usb-dra-kubeletplugin/ 2>&1 | tail -3`
  Expected: FAIL (build errors: `undefined: newDriverCore`, `assignAliases`, unknown field `TTYs`).

- [ ] **Step 3: Implement** per the spec's Prepare, Unprepare and Prepared state sections. Decided here:
  - `prepareOne` holds `d.mu` for its whole body; steps in the spec's order 1–5; the checkpoint is saved after the CDI spec is written, and a save error fails the claim.
  - `unprepareOne` holds `d.mu`; `cdi.deleteClaimSpec(uid)` always, then delete the checkpoint entry and save only if it existed.
  - Admin access is `res.AdminAccess != nil && *res.AdminAccess`; error text `admin access is not supported`.
  - `assignAliases` keys counters by `resourceclaim.BaseRequestRef(dev.Requests[0])` (`k8s.io/dynamic-resource-allocation/resourceclaim`).
  - To make `TestRunRejectsBadFlags` possible, split `run()` into `parseFlags(args []string) (config, error)` and the rest; `parseFlags` uses its own `flag.FlagSet` (`flag.ContinueOnError`).

- [ ] **Step 4: Verify**
  Run: `task test && golangci-lint run && go mod tidy -diff`
  Expected: all `ok`, `No issues found`, no diff.

- [ ] **Step 5: Commit** `feat(kubeletplugin): inject ttys with aliases and persist prepared claims`.

---

### Task 7: Documentation and invariants

Kind: rewrite

**Files:**
- Modify: `README.md`
- Modify: `.claude/CLAUDE.md`

**Interfaces:**
- Consumes: everything above (names, flags, attributes, manifests).

- [ ] **Step 1: Rewrite `README.md`** for usb-dra: what it publishes; the exclusion rule and both flags; identity (serial vs port, the `identity` attribute); attributes table; the CEL selector rules with the three shipped examples spelled out in full; tty injection and the `/dev/dra/<request>/tty<k>` alias; env vars; checkpoint location; the Known limitations list from the spec verbatim in substance; the usb-serial module note ("if autoload fails, load `ch341`/`cp210x`/`ftdi_sio`/`pl2303` via Talos `machine.kernel.modules`"); install commands (`kubectl apply -f deploy/daemonset.yaml -f deploy/deviceclasses.yaml -f deploy/claims.yaml`).

- [ ] **Step 2: Rewrite `.claude/CLAUDE.md`**: project name and goal for usb-dra; the spec's 12 invariants verbatim, each followed by the name of the test that asserts it; Layout section for the new files; the pin rule "`k8s.io/*` stays v0.34.x while the cluster runs 1.35; bumping is a separate change"; keep the Deployment gotchas and Git identity sections unchanged.

- [ ] **Step 3: Verify**
  Run: `for t in $(grep -oE 'Test[A-Za-z0-9_]+' .claude/CLAUDE.md | sort -u); do grep -rqw "func $t" --include='*_test.go' . || echo "MISSING $t"; done; npx -y markdownlint-cli2 README.md .claude/CLAUDE.md docs/designs/2026-11-13-usb-dra-design.md`
  Expected: no `MISSING` lines; `0 issues`.

- [ ] **Step 4: Commit** `docs: describe usb-dra and its invariants`.
