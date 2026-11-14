# usb-dra Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the RTL-SDR-only DRA driver into a generic USB DRA driver (`usb.jacaudi.dev`) that publishes every eligible local USB device, injects serial ttys with stable aliases, and keeps prepared state across restarts.

**Architecture:** `internal/usb` owns sysfs scanning, filtering, identity/naming and ResourceSlice attributes as pure functions over `fs.FS`. `cmd/usb-dra-kubeletplugin` owns the kubelet plugin: a versioned JSON checkpoint, prepare/unprepare with per-claim CDI specs, and flag wiring. Device typing lives in `deploy/` DeviceClass CEL selectors, tested with `k8s.io/dynamic-resource-allocation/cel`.

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
- New dependencies: allowed only with a present requirement, pinned, named in the report (see Global Constraints for the one accepted set).
- Dependency bump to clear govulncheck: in scope as Task 9 (user, 2026-11-14).
- Boundaries: 8 hours wall clock; dispatch cap `6 × tasks + 12`; 2 hours without a task completing (defaults, user 2026-11-13).
- Worktree after (a) merge: remove (not applicable under (c)).
- Already done before this plan (user-authorized): GitHub repo renamed `jacaudi/rtlsdr-dra` → `jacaudi/usb-dra`, remote URL updated; sysfs fixtures committed (`c6f0425`).
- Commits are signed. A signing failure is `PLAN: PAUSED — signing`; never `--no-gpg-sign` or `-c commit.gpgsign=false`.

## Global Constraints

- Versions: `k8s.io/*` stays v0.34.3; `tags.cncf.io/container-device-interface` stays v1.1.0. Task 5's `go mod tidy` adds the test-only modules that `k8s.io/dynamic-resource-allocation/cel` needs (`k8s.io/apiserver` v0.34.3, `k8s.io/component-base` v0.34.3, `github.com/google/cel-go` v0.26.0, `cel.dev/expr` and their dependencies) and bumps `github.com/fsnotify/fsnotify` v1.5.1 → v1.9.0; that diff is accepted (ledger ruling). Task 9 bumps the vulnerable dependencies (user request 2026-11-14). No other module requirement may change. Imports from modules already required (`k8s.io/dynamic-resource-allocation/resourceclaim`, `k8s.io/apimachinery/pkg/util/yaml`, `sigs.k8s.io/yaml`) are allowed in any code.
- Names: driver `usb.jacaudi.dev`; module `github.com/jacaudi/usb-dra`; CDI vendor `k8s.usb.jacaudi.dev`, class `usb`; env prefix `USB_`.
- Never publish device node, bus number, devnum, tty names or tty aliases as attributes.
- CDI device nodes carry `Path`, `HostPath` and `Permissions: "rw"` only: no `Type`, `Major`, `Minor`, `UID`, `GID`, `FileMode`.
- ResourceSlice validity: device names are DNS-1123 labels ≤ 63 and unique; device count capped with `resourceapi.ResourceSliceMaxDevices`; string attributes bounded with `resourceapi.DeviceAttributeMaxValueLength` (never literal `128` / `64`).
- Pointer literals use Go 1.26 `new(value)`; do not add `k8s.io/utils/ptr`.
- New or changed code comments are single lines, used where Go convention needs them (package and exported-identifier doc comments).
- After every task: `task test` passes; `golangci-lint run` exits 0 (it prints `0 issues.`, or `golangci-lint: No issues found` when the rtk hook rewrites it); `go mod tidy -diff` prints nothing.
- Test fixtures live under `internal/usb/testdata/sysfs/`. `.dockerignore` strips `**/testdata` and `**/*_test.go`, so the Docker build runs no tests; this plan does not change that.
- **Commit step format** (every task's last step): Run: `git add <the task's listed paths> && git commit -m "<message>" -m "Co-Authored-By: Claude <noreply@anthropic.com>" && git cat-file commit HEAD | grep -c '^gpgsig'` — Expected: `1`. A signing error is `PLAN: PAUSED — signing`.

## Review Focus

- A device that unplugs mid-scan (attribute files vanish between reads) must not fail the scan or another claim's prepare — pinned in Task 3 (`TestScanSkipsUnreadableInterface`) and Task 7 (`TestPrepareOneUnreadableDeviceDoesNotAffectOtherClaim`).
- A claim pinned by serial when that serial becomes duplicated must stop matching instead of matching either stick — pinned in Task 5 (`TestSelectorsSerialPinSkipsDuplicate`).
- After a node reboot `/var/run/cdi` is empty while the checkpoint survives; re-prepare must rewrite the spec — pinned in Task 7 (`TestPrepareRewritesSpecForKnownClaim`).
- A corrupt or future-version checkpoint must stop the driver at startup, and a failed save must not leave state that blocks the device later — pinned in Task 6 (`TestLoadCheckpointRejectsUnknownVersion`, `TestLoadCheckpointRejectsGarbage`) and Task 7 (`TestSaveFailureLeavesStateUnchanged`).
- Interface numbers ≥ 10 (`0a`) and `ttyUSB10` must order numerically, or alias `tty<k>` numbering shifts — pinned in Task 3 (`TestScanOrdersInterfacesAndTTYsNumerically`).

---

### Task 1: Rename to usb-dra

Kind: rewrite

**Files:**
- Modify: `go.mod`
- Modify: `internal/rtlsdr/usb.go` (moves to `internal/usb/usb.go`)
- Modify: `internal/rtlsdr/slice.go` (moves to `internal/usb/slice.go`)
- Modify: `internal/rtlsdr/usb_test.go` (moves to `internal/usb/usb_test.go`)
- Modify: `internal/rtlsdr/usb_real_test.go` (moves to `internal/usb/usb_real_test.go`)
- Modify: `internal/rtlsdr/slice_test.go` (moves to `internal/usb/slice_test.go`)
- Modify: `cmd/rtlsdr-dra-kubeletplugin/cdi.go` (moves to `cmd/usb-dra-kubeletplugin/cdi.go`)
- Modify: `cmd/rtlsdr-dra-kubeletplugin/cdi_test.go` (moves to `cmd/usb-dra-kubeletplugin/cdi_test.go`)
- Modify: `cmd/rtlsdr-dra-kubeletplugin/driver.go` (moves to `cmd/usb-dra-kubeletplugin/driver.go`)
- Modify: `cmd/rtlsdr-dra-kubeletplugin/driver_test.go` (moves to `cmd/usb-dra-kubeletplugin/driver_test.go`)
- Modify: `cmd/rtlsdr-dra-kubeletplugin/main.go` (moves to `cmd/usb-dra-kubeletplugin/main.go`)
- Modify: `deploy/daemonset.yaml`
- Modify: `Dockerfile`
- Move: `testdata/sysfs/talos-05-wrk.tsv`, `testdata/sysfs/talos-01-cp.tsv` → `internal/usb/testdata/sysfs/`

**Interfaces:**
- Produces: package `github.com/jacaudi/usb-dra/internal/usb` with `const DriverName = "usb.jacaudi.dev"` and `const SysfsUSBDir = "sys/bus/usb/devices"`. Everything else keeps its current name and behavior (discovery still RTL-SDR-only, `DeviceName()` still `rtlsdr-<serial>`).

- [ ] **Step 1: Move the files**
  Run: `git mv internal/rtlsdr internal/usb && git mv cmd/rtlsdr-dra-kubeletplugin cmd/usb-dra-kubeletplugin && git mv testdata internal/usb/testdata && git status --short | head -20`
  Expected: `R` (rename) lines for every file listed above; no errors.

- [ ] **Step 2: Change expected values in the moved tests** (every `*_test.go` under `cmd/usb-dra-kubeletplugin/` and `internal/usb/`), in this order (the first must precede the second): `k8s.sdr.jacaudi.dev/rtlsdr` → `k8s.usb.jacaudi.dev/usb`; `sdr.jacaudi.dev` → `usb.jacaudi.dev`; every `RTLSDR_SERIAL` → `USB_SERIAL` and `RTLSDR_DEVNODE` → `USB_DEVNODE` (including inside `t.Errorf` messages); package clause `package rtlsdr` → `package usb`; import `github.com/jacaudi/rtlsdr-dra/internal/rtlsdr` → `github.com/jacaudi/usb-dra/internal/usb` and identifier `rtlsdr.` → `usb.`.

- [ ] **Step 3: Run to verify failure**
  Run: `go test ./... 2>&1 | tail -5`
  Expected: FAIL (build errors: module path `github.com/jacaudi/usb-dra` not found, or package clause mismatch).

- [ ] **Step 4: Apply the rename in production code**
  `go.mod` module `github.com/jacaudi/usb-dra`; `package usb` in `usb.go`/`slice.go`; `DriverName = "usb.jacaudi.dev"`; `cdiClass = "usb"`; env keys `USB_SERIAL`, `USB_DEVNODE`; in `main.go` and `driver.go` the import becomes `github.com/jacaudi/usb-dra/internal/usb` and every `rtlsdr.` identifier becomes `usb.`. Exact comment edits: the package comment in `internal/usb/usb.go` becomes the single line `// Package usb discovers USB devices from sysfs and models them as DRA devices.`; the doc comment at the top of `main.go` becomes the single line `// Command usb-dra-kubeletplugin publishes local USB devices as DRA devices and maps them into containers via CDI.`; in `cdi.go` the `rtl_tcp -d "$RTLSDR_SERIAL"` comment text becomes `rtl_tcp -d "$USB_SERIAL"`; in `slice.go` the doc example `device.attributes["sdr.jacaudi.dev"]` becomes `device.attributes["usb.jacaudi.dev"]`. `deploy/daemonset.yaml`: every `rtlsdr-dra` → `usb-dra`, `--driver-name=usb.jacaudi.dev`, image `ghcr.io/jacaudi/usb-dra:latest`. `Dockerfile`: build `-o /usb-dra-kubeletplugin ./cmd/usb-dra-kubeletplugin`, `COPY --from=build /usb-dra-kubeletplugin /usr/bin/usb-dra-kubeletplugin`, `ENTRYPOINT ["/usr/bin/usb-dra-kubeletplugin"]`.

- [ ] **Step 5: Verify**
  Run: `task test && golangci-lint run && go mod tidy -diff && grep -rnE 'rtlsdr-dra|sdr\.jacaudi\.dev|RTLSDR_|internal/rtlsdr' cmd internal deploy/daemonset.yaml Dockerfile go.mod .github taskfile.yml .taskfiles; echo "grep-exit=$?"`
  Expected: `ok` for both packages, lint exit 0, no tidy output, no grep matches, `grep-exit=1`.

- [ ] **Step 6: Commit** — paths: `go.mod cmd internal deploy/daemonset.yaml Dockerfile` (the `git mv` renames, including `internal/usb/testdata`, are already staged); message `feat(usb-dra)!: rename driver to usb.jacaudi.dev`.

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
  - `func (f Filter) Publish(vendor, product, rawSerial string, interfaceClasses []string) bool` — `rawSerial` is the trimmed sysfs `serial` content, `""` when absent

- [ ] **Step 1: Write the failing tests**

```go
func TestParseExcludeClasses(t *testing.T) {
	got, err := ParseExcludeClasses(DefaultExcludeClasses)
	// err == nil; got == map[string]bool{"03": true, "08": true, "e0": true}
	got, err = ParseExcludeClasses("")
	// err == nil; len(got) == 0
	for _, bad := range []string{"3", "0g", "03,,08", "E0", "003"} {
		_, err = ParseExcludeClasses(bad)
		// err != nil and err.Error() contains bad's offending token
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
	// f.Publish("0e8d", "c616", "000000000", []string{"e0", "e0", "e0"}) == false
	// f.Publish("239a", "8029", "X", []string{"02", "0a"}) == true
	// f.Publish("1a2b", "3c4d", "", []string{"03", "ff"}) == true
	// f.Publish("046d", "c52b", "", []string{"03"}) == true
	// f.Publish("1234", "5678", "ABC", []string{"03"}) == true
	// f.Publish("1234", "5678", "other", []string{"03"}) == false
	// Filter{}.Publish("0e8d", "c616", "", []string{"e0"}) == true
}
```

- [ ] **Step 2: Run to verify failure**
  Run: `go test ./internal/usb/ -run 'TestParse|TestFilter' 2>&1 | tail -15`
  Expected: FAIL, build error `undefined: ParseExcludeClasses` (this compile failure is the RED state; add no stubs).

- [ ] **Step 3: Implement the signatures above in `internal/usb/filter.go`**
  Class tokens match `^[0-9a-f]{2}$`; vid/pid match `^[0-9a-f]{4}$`. `Publish` returns true when an include matches (vid/pid equal; serial compared with `strings.EqualFold`; an include with empty serial matches any), else false only when `interfaceClasses` is non-empty and every element is in `ExcludeClasses`, else true.

- [ ] **Step 4: Verify**
  Run: `task test && golangci-lint run && go mod tidy -diff`
  Expected: all `ok`, lint exit 0, no diff.

- [ ] **Step 5: Commit** — paths: `internal/usb/filter.go internal/usb/filter_test.go`; message `feat(usb): add device class filter and include overrides`.

---

### Task 3: Sysfs scan

**Files:**
- Create: `internal/usb/sysfs.go`
- Create: `internal/usb/fixture_test.go`
- Test: `internal/usb/sysfs_test.go`

**Interfaces:**
- Consumes: `Filter` (Task 2); existing `readAttr`, `readIntAttr`, `SysfsUSBDir` in `internal/usb/usb.go`.
- Produces:
  - `type Interface struct{ Number int; Class string; TTYs []string }` — `Class` two lowercase hex digits; `TTYs` kernel names (`ttyACM0`), numeric order
  - `type candidate struct{ Dir, Vendor, Product, RawSerial, Manufacturer, ProductName, Port string; Interfaces []Interface; Bus, Dev int }` — `Vendor`/`Product` lowercased; `RawSerial`, `Manufacturer`, `ProductName` trimmed raw sysfs strings (`""` when the file is absent)
  - `type Skip struct{ Dir, Reason string; Severe bool }`
  - `func scan(fsys fs.FS, f Filter) ([]candidate, []Skip, error)` — candidates sorted by `Dir`; error only when `SysfsUSBDir` cannot be read
  - `func portRevision(speed string) (string, bool)`
  - test helpers in `fixture_test.go`: `func loadFixture(t *testing.T, name string) fstest.MapFS` (reads `testdata/sysfs/<name>.tsv`; each line `path<TAB>content` becomes `fsys[path] = &fstest.MapFile{Data: []byte(content + "\n")}`); `func withFile(fsys fstest.MapFS, rel, content string)` sets `fsys[SysfsUSBDir+"/"+rel]` to `content + "\n"`; `func without(fsys fstest.MapFS, rel string)` deletes the key `SysfsUSBDir+"/"+rel` and every key under it (`… + "/"`). All test paths such as `5-1/serial` or `5-1:1.0` are relative to `SysfsUSBDir`. Every skip test asserts the exact `Reason`.

- [ ] **Step 1: Write the fixture helpers and the failing tests**

```go
var defaultFilter = Filter{ExcludeClasses: map[string]bool{"03": true, "08": true, "e0": true}}

func TestScanTalos05(t *testing.T) {
	cands, skips, err := scan(loadFixture(t, "talos-05-wrk"), defaultFilter)
	// err == nil; len(skips) == 0
	// Dirs == []string{"11-1", "5-1", "9-1"}
	// 5-1: Vendor "239a", Product "8029", RawSerial "FD14948954CD1F4F", Manufacturer "RAKwireless",
	//      ProductName "WisCore RAK4631 Board", Port "PNP0D10:02-v2-1", Bus 5, Dev 2,
	//      Interfaces == []Interface{{0, "02", []string{"ttyACM0"}}, {1, "0a", nil}}
	// 11-1: Port "PNP0D10:05-v2-1", Bus 11, Dev 2, Interfaces == []Interface{{0, "ff", nil}}
	// 9-1:  Port "PNP0D10:04-v2-1", Interfaces == []Interface{{0, "ff", nil}, {1, "ff", nil}}
}
func TestScanTalos05NoExclusions(t *testing.T)   // Filter{} → also 15-1 with Port "PNP0D10:08-v2-1", RawSerial "000000000"
func TestScanTalos01HubsOnly(t *testing.T)       // talos-01-cp, Filter{} → 0 candidates, 0 skips, err == nil
func TestScanSkipsVhciSilently(t *testing.T)
	// add device 17-1 under the fixture's usb17 (serial "vhci_hcd.0"): idVendor "abcd", idProduct "0001",
	// busnum "17", devnum "2", bDeviceClass "00", 17-1:1.0 bInterfaceClass "ff", bInterfaceNumber "00"
	// → absent from cands and from skips
func TestScanSkipsUnconfigured(t *testing.T)     // without "5-1:1.0" and without "5-1:1.1" → Skip{Dir "5-1", Reason "no interfaces", Severe false}
func TestScanSkipsUnreadableDevice(t *testing.T) // 5-1/devnum "x" → Skip{Dir "5-1", Reason "unreadable device", Severe false}
func TestScanSkipsUnreadableInterface(t *testing.T)
	// without "5-1:1.1/bInterfaceClass" → Skip{Dir "5-1", Reason "unreadable interface", Severe false}; 9-1 and 11-1 still in cands; err == nil
func TestScanSkipsBadInterfaceNumber(t *testing.T) // 5-1:1.1/bInterfaceNumber "zz" → Skip{Dir "5-1", Reason "unreadable interface", Severe false}
func TestScanSkipsUnknownSpeed(t *testing.T)     // usb5/speed "unknown" → Skip{Dir "5-1", Reason "unknown root hub speed", Severe false}
func TestScanSkipsOverlongPort(t *testing.T)     // usb5/serial = 70 × "a" → Skip{Dir "5-1", Reason "port longer than 64 bytes", Severe true}
func TestScanMissingRootIsError(t *testing.T)    // fstest.MapFS{} → err != nil
func TestScanOrdersInterfacesAndTTYsNumerically(t *testing.T)
	// synthetic device 3-1 (idVendor "1a86", idProduct "7523", busnum "3", devnum "4", bDeviceClass "00")
	// under usb3 (serial "0000:00:14.0", speed "480", bDeviceClass "09"):
	// dirs 3-1:1.10 (bInterfaceNumber "0a", class "ff", entries ttyUSB10/dev and ttyUSB2/dev)
	// and 3-1:1.2 (bInterfaceNumber "02", class "02", tty/ttyACM3/dev)
	// → Interfaces[0] == {2, "02", ["ttyACM3"]}, Interfaces[1] == {10, "ff", ["ttyUSB2", "ttyUSB10"]}
func TestPortRevision(t *testing.T)
	// "1.5"→("1",true), "12"→("1",true), "480"→("2",true), "5000"/"10000"/"20000"→("3",true), "unknown"→("",false)
```

- [ ] **Step 2: Run to verify failure**
  Run: `go test ./internal/usb/ -run 'TestScan|TestPortRevision' 2>&1 | tail -15`
  Expected: FAIL, build error `undefined: scan`.

- [ ] **Step 3: Implement `scan` and `portRevision` in `internal/usb/sysfs.go`** per the spec's Discovery and Controller/revision/port sections. Decided here:
  - Device dirs: entries of `SysfsUSBDir` without `:` that have `idVendor`, `idProduct`, `busnum`, `devnum`; others are ignored silently. A device whose `bDeviceClass` is unreadable or whose `busnum`/`devnum` is not an integer → Skip `unreadable device` (Severe false).
  - Check order per device: hub check, `Filter.Publish` (silent), then root hub reads (controller, speed) and port.
  - Silent (no Skip): `bDeviceClass == "09"` (hub); controller (`usb<busnum>/serial`) starting with `vhci_hcd`; `Filter.Publish` false.
  - Interfaces of `D`: entries with prefix `D + ":"`. `bInterfaceNumber` via `strconv.ParseUint(v, 16, 8)`; `bInterfaceClass` stored as the two-digit lowercase string after validating it parses as hex.
  - ttys: names under `<intf>/tty/`, plus entries of `<intf>` whose name starts with `ttyUSB`; sorted by the integer after the trailing letters.
  - Skip reasons (exact strings): `unreadable device` (Severe false), `no interfaces` (Severe false), `unreadable interface` (false), `unknown root hub speed` (false), `unreadable root hub` (false, when `usb<busnum>/serial` or `speed` is missing), `port longer than 64 bytes` (true, use `resourceapi.DeviceAttributeMaxValueLength`).
  - Port = `<controller>-v<rev>-<chain>`, chain = device dir name after `<busnum>-`.

- [ ] **Step 4: Verify**
  Run: `task test && golangci-lint run && go mod tidy -diff`
  Expected: all `ok`, lint exit 0, no diff.

- [ ] **Step 5: Commit** — paths: `internal/usb/sysfs.go internal/usb/sysfs_test.go internal/usb/fixture_test.go`; message `feat(usb): scan sysfs for every usb device with interfaces, ttys and port`.

---

### Task 4: Identity, names and attributes

Kind: rewrite

**Files:**
- Modify: `internal/usb/usb.go` (rewrite on top of `scan`; keep `readAttr`/`readIntAttr`/`SysfsUSBDir`)
- Create: `internal/usb/identity.go`
- Modify: `internal/usb/slice.go`
- Modify: `internal/usb/usb_test.go` (rewrite)
- Modify: `internal/usb/slice_test.go` (rewrite)
- Modify: `internal/usb/usb_real_test.go` (delete: its capture cannot be re-taken; replaced by the TSV fixtures)
- Create: `internal/usb/identity_test.go`
- Modify: `cmd/usb-dra-kubeletplugin/driver.go`
- Modify: `cmd/usb-dra-kubeletplugin/main.go`
- Modify: `cmd/usb-dra-kubeletplugin/driver_test.go`

**Interfaces:**
- Consumes: `scan`, `candidate`, `Interface`, `Skip` (Task 3); `Filter`, `DefaultExcludeClasses`, `ParseExcludeClasses` (Task 2).
- File placement: `Identity`, `Device` and its methods, `Scan`, `Discover`, `FindByName` in `usb.go`; `deviceName` and the uniqueness pass in `identity.go`; `Attributes`, `NodeResources` in `slice.go`. Tests: `TestDiscover*`, `TestDuplicate*`, `TestCaseFolded*`, `TestPortCollision*`, `Test*SerialIsPortIdentity`, `TestFindByName` in `usb_test.go`; `TestDeviceNameSanitizes`, `TestLongNameIsTruncatedWithHash` in `identity_test.go`; `TestAttributes*`, `TestNodeResources*` in `slice_test.go` (replacing its current contents, including the old `TestNodeResourcesPublishesEmptyPool`).
- Produces:
  - `type Identity string` with `IdentitySerial Identity = "serial"`, `IdentityPort Identity = "port"`
  - `type Device struct{ Name string; Identity Identity; Vendor, Product, Serial, Port, Manufacturer, ProductName string; Interfaces []Interface; Bus, Dev int }` — `Serial` = raw serial if non-empty, valid UTF-8, ≤ `DeviceAttributeMaxValueLength` bytes and not `^0+$` (kept for duplicates), else `""`; `Manufacturer`/`ProductName` = `""` if invalid UTF-8, else truncated on a rune boundary to ≤ 64 bytes
  - `func (d Device) DevNode() string` → `fmt.Sprintf("/dev/bus/usb/%03d/%03d", Bus, Dev)`
  - `func (d Device) InterfaceClasses() []string` (sorted unique)
  - `func (d Device) TTYs() []string` (`/dev/<name>`, interface order then tty order)
  - `type Scan struct{ Devices []Device; DuplicateSerials []string; Skipped []Skip }` (Devices sorted by Name, uncapped)
  - `func Discover(fsys fs.FS, f Filter) (Scan, error)`
  - `func FindByName(fsys fs.FS, f Filter, name string) (Device, error)` — not-found error text contains `no longer attached`
  - `func deviceName(vendor, product, id string) string`
  - `func (d Device) Attributes() map[resourceapi.QualifiedName]resourceapi.DeviceAttribute`
  - `func NodeResources(nodeName string, devices []Device) (resourceslice.DriverResources, int)` — second value is the count dropped by the `ResourceSliceMaxDevices` cap
- Removes: `FindBySerial`, the `supported` allowlist, `Device.DeviceName()`, `readDevice`.

- [ ] **Step 1: Write the failing tests**

```go
func TestDiscoverTalos05(t *testing.T) {
	scan, err := Discover(loadFixture(t, "talos-05-wrk"), defaultFilter)
	// names == []string{"usb-0bda-2838-35650102", "usb-0bda-2838-93610062", "usb-239a-8029-fd14948954cd1f4f"}
	// RAK: Identity serial, DevNode() "/dev/bus/usb/005/002", TTYs() == []string{"/dev/ttyACM0"}
	// 93610062: InterfaceClasses() == []string{"ff"}; len(scan.DuplicateSerials) == 0
}
func TestDiscoverMediaTekPortIdentity(t *testing.T) // Filter{} → "usb-0e8d-c616-p-pnp0d10-08-v2-1", Identity port, Serial ""
func TestDuplicateSerialsFallBackToPort(t *testing.T)
	// set 9-1/serial and 11-1/serial to "00000001" → both Identity port, both Serial "00000001",
	// names "usb-0bda-2838-p-pnp0d10-04-v2-1" and "usb-0bda-2838-p-pnp0d10-05-v2-1", DuplicateSerials == []string{"00000001"}
func TestCaseFoldedSerialsCollide(t *testing.T)    // 9-1 serial "AbC", 11-1 serial "abc" → both Identity port; DuplicateSerials == []string{"AbC", "abc"}
func TestPortCollisionSkipsBoth(t *testing.T)
	// synthetic usb3 and usb4 both serial "0000:00:14.0", speed "480", bDeviceClass "09"; serial-less devices 3-1 and 4-1
	// (vid:pid 1a86:7523, bDeviceClass "00", one interface class "ff") → neither in Devices; Skipped has both with
	// Reason "name collision", Severe true
func TestMissingSerialIsPortIdentity(t *testing.T)   // without "5-1/serial" → Name "usb-239a-8029-p-pnp0d10-02-v2-1", Identity port, Serial ""
func TestAllZeroSerialIsPortIdentity(t *testing.T)   // 5-1/serial "000000000" → Identity port, Serial ""
func TestOverlongSerialIsPortIdentity(t *testing.T)  // 5-1/serial = 65 × "A" → Identity port, Serial ""
func TestInvalidUTF8SerialIsPortIdentity(t *testing.T) // 5-1/serial "\xff\xfe" → Identity port, Serial ""
func TestLongNameIsTruncatedWithHash(t *testing.T)
	// 5-1/serial s = 60 lowercase alnum chars; full := "usb-239a-8029-" + s;
	// want := full[:54] + "-" + hex.EncodeToString(sha256.Sum256([]byte(full)))[:8]; Name == want; len(Name) == 63
func TestDeviceNameSanitizes(t *testing.T)
	// deviceName("239a", "8029", "A_b.C--d") == "usb-239a-8029-a-b-c-d"; deviceName("1a86", "7523", "p-0000:00:14.0-v2-1.4") == "usb-1a86-7523-p-0000-00-14-0-v2-1-4"
func TestAttributesRAK(t *testing.T)
	// exactly these keys/values: vendor "239a", product "8029", identity "serial", port "PNP0D10:02-v2-1",
	// serial "FD14948954CD1F4F", manufacturer "RAKwireless", productName "WisCore RAK4631 Board", interfaceClasses "02,0a"
func TestAttributesPortIdentityHasNoSerial(t *testing.T) // MediaTek: no "serial" key; identity "port"
func TestAttributesBoundStrings(t *testing.T)
	// 5-1/manufacturer = 70 × "a" → 64 bytes; = 63 × "a" + "é" → 63 bytes (rune boundary); = "\xff\xfe" → key absent;
	// a device with 22 interfaces of distinct classes (interfaceClasses would be 65 bytes) → key absent
func TestAttributesNeverExposeNodes(t *testing.T)  // keys ∩ {"devnode","bus","busnum","devnum","tty","ttys"} == ∅
func TestNodeResourcesCapsAt128(t *testing.T)      // 130 synthetic Devices → one slice with 128 devices, dropped == 2
func TestNodeResourcesPublishesEmptyPool(t *testing.T) // nil → pool named after the node, one slice, 0 devices, dropped == 0
func TestFindByName(t *testing.T)                  // "usb-239a-8029-fd14948954cd1f4f" found; "usb-dead-beef-x" → err contains "no longer attached"
```

- [ ] **Step 2: Run to verify failure**
  Run: `go test ./internal/usb/ 2>&1 | tail -15`
  Expected: FAIL (build errors on `Discover`'s new signature and `Device` fields).

- [ ] **Step 3: Implement** per the spec's Identity and device names, Attributes sections. Decided here:
  - `deviceName`: `"usb-" + vendor + "-" + product + "-" + id`, lowercase; `[^a-z0-9-]` → `-`; collapse `-+`; trim `-`; if > 63, `name[:54] + "-" + first 8 hex of sha256(name)` where `name` is the sanitized untruncated string.
  - Port-identity id is `"p-" + Port`.
  - Uniqueness: build names; any group of ≥ 2 serial-identity devices sharing a name → all switch to port identity, each one's raw serial added to `DuplicateSerials` (sorted, unique); rebuild names; any remaining group of ≥ 2 → all removed, each added to `Skipped` with Reason `name collision`, Severe true.
  - Attribute keys are bare (`"vendor"`), qualified implicitly by the driver name, as today.
  - `driver.go` call sites: `lookup` uses `usb.FindByName(d.sysfs, d.filter, res.Device)`; `preparedDevice.Serial` set only when `Identity == usb.IdentitySerial`; `publish` logs each `DuplicateSerials` entry and each `Skipped` entry with `Severe` at error level, other `Skipped` entries at `V(2)`, and the dropped count at error level when > 0.
  - `config` gains `filter usb.Filter`; `run()` sets `ExcludeClasses` from `usb.ParseExcludeClasses(usb.DefaultExcludeClasses)`.
  - `driver_test.go`'s fake sysfs builder writes, per device: `bDeviceClass` `00`; interface `<dir>:1.0` with `bInterfaceClass` `ff`, `bInterfaceNumber` `00`; and root hub `usb<bus>` with `serial` `0000:00:14.0`, `speed` `480`, `bDeviceClass` `09`. Device names in those tests become `usb-0bda-2838-<serial>`.

- [ ] **Step 4: Verify**
  Run: `task test && golangci-lint run && go mod tidy -diff`
  Expected: all `ok`, lint exit 0, no diff.

- [ ] **Step 5: Commit** — paths: `internal/usb cmd/usb-dra-kubeletplugin`; message `feat(usb): publish every eligible usb device with stable identity`.

---

### Task 5: DeviceClasses, example claims and selector tests

Kind: rewrite

**Files:**
- Create: `deploy/deviceclasses.yaml`
- Modify: `deploy/claims.yaml` (rewrite)
- Modify: `go.mod`
- Modify: `go.sum`
- Create: `internal/usb/selectors_test.go`

**Interfaces:**
- Consumes: `Discover`, `Device.Attributes()`, `DriverName` (Task 4).

- [ ] **Step 1: Write the failing tests** in `internal/usb/selectors_test.go`.
  `func loadSelectors(t *testing.T) (classes map[string]string, claims map[string]claimSel)` with `type claimSel struct{ Class, Expr string }`: split `../../deploy/deviceclasses.yaml` and `../../deploy/claims.yaml` into documents with `yaml.NewYAMLReader(bufio.NewReader(f))` (`k8s.io/apimachinery/pkg/util/yaml`), decode each with `sigs.k8s.io/yaml.UnmarshalStrict` into `resourceapi.DeviceClass` / `resourceapi.ResourceClaimTemplate`; collect each class's `spec.selectors[0].cel.expression` keyed by class name, and each template's `spec.spec.devices.requests[0].exactly` class name and first selector expression (`""` if none) keyed by template name. Compile with `cel.GetCompiler(cel.Features{}).CompileCELExpression(expr, cel.Options{})`; evaluate with `result.DeviceMatches(ctx, cel.Device{Driver: drv, Attributes: attrs})`.
  Fixture devices: the RAK and both RTL-SDRs from `Discover(loadFixture(t, "talos-05-wrk"), defaultFilter)`; `dupe` = a `0bda:2838` port-identity Device with `Serial` `"35650102"` and Port `"PNP0D10:09-v2-1"`; `noserial` = a `0bda:2838` port-identity Device with `Serial` `""` and Port `"PNP0D10:07-v2-1"`; `foreign` = `cel.Device{Driver: "gpu.example.com", Attributes: {"model": {StringValue: new("x")}}}`.

```go
func TestSelectorsCompile(t *testing.T)          // every non-empty class and claim expression: result.Error == nil
func TestClassSelectorsNeverError(t *testing.T)  // every class expr × every fixture device: err == nil
func TestClassSelectorsMatch(t *testing.T)
	// rtl-sdr.usb.jacaudi.dev matches exactly {both RTL-SDRs, dupe, noserial}; rak4631.usb.jacaudi.dev matches exactly {RAK}
func TestClaimSelectorsMatch(t *testing.T)
	// each claim expr evaluated only on devices its class admits; err == nil for every evaluation;
	// rtl-sdr-any: Expr == ""; rtl-sdr-35650102 matches only the serial-identity 35650102;
	// rak4631-lora-port matches only the RAK
func TestSelectorsSerialPinSkipsDuplicate(t *testing.T) // rtl-sdr-35650102 does not match dupe and returns no error on noserial
```

- [ ] **Step 2: Tidy the module**
  Run: `go mod tidy && git diff --stat go.mod go.sum && git diff go.mod | grep -E '^[+-]\s' | head -40`
  Expected: `go.mod` gains require lines including `github.com/google/cel-go v0.26.0`, `k8s.io/apiserver v0.34.3`, `k8s.io/component-base v0.34.3`; `github.com/fsnotify/fsnotify` changes `v1.5.1` → `v1.9.0`; no `k8s.io/*` line other than those additions changes version. Any other version change is a stop: `PLAN: BLOCKED — every path is a guess: unexpected go.mod change`.

- [ ] **Step 3: Run to verify failure**
  Run: `go test ./internal/usb/ -run 'Selector' 2>&1 | tail -15`
  Expected: FAIL, error opening `../../deploy/deviceclasses.yaml` (no such file).

- [ ] **Step 4: Write the manifests**, spelling out `device.attributes["usb.jacaudi.dev"]`; one-line comment at the top of each file:
  - `deploy/deviceclasses.yaml` (`resource.k8s.io/v1`, `DeviceClass`): `rtl-sdr.usb.jacaudi.dev` with `device.driver == "usb.jacaudi.dev" && device.attributes["usb.jacaudi.dev"].vendor == "0bda" && device.attributes["usb.jacaudi.dev"].product in ["2838", "2832"]`; `rak4631.usb.jacaudi.dev` with `device.driver == "usb.jacaudi.dev" && device.attributes["usb.jacaudi.dev"].vendor == "239a" && device.attributes["usb.jacaudi.dev"].product == "8029"`.
  - `deploy/claims.yaml` (`ResourceClaimTemplate`, namespace `selfhosted`): `rtl-sdr-any` (request `radio`, `exactly: {deviceClassName: rtl-sdr.usb.jacaudi.dev, allocationMode: ExactCount, count: 1}`); `rtl-sdr-35650102` (same plus selector `device.attributes["usb.jacaudi.dev"].identity == "serial" && device.attributes["usb.jacaudi.dev"].serial == "35650102"`); `rak4631-lora-port` (request `lora`, class `rak4631.usb.jacaudi.dev`, selector `device.attributes["usb.jacaudi.dev"].port == "PNP0D10:02-v2-1"`).

- [ ] **Step 5: Verify**
  Run: `task test && golangci-lint run && go mod tidy -diff && yamllint deploy/`
  Expected: all `ok`, lint exit 0, no diff, yamllint exit 0.

- [ ] **Step 6: Commit** — paths: `deploy/deviceclasses.yaml deploy/claims.yaml go.mod go.sum internal/usb/selectors_test.go`; message `feat(deploy): add usb deviceclasses and guarded example claims`.

---

### Task 6: Checkpoint file

**Files:**
- Create: `cmd/usb-dra-kubeletplugin/checkpoint.go`
- Test: `cmd/usb-dra-kubeletplugin/checkpoint_test.go`

**Interfaces:**
- Produces:
  - `const checkpointFile = "checkpoint.json"`, `const checkpointVersion = 1`
  - `type checkpointDevice struct{ Device string `json:"device"`; Request string `json:"request"`; Port string `json:"port"` }`
  - `type checkpointClaim struct{ Namespace string `json:"namespace"`; Name string `json:"name"`; Devices []checkpointDevice `json:"devices"` }`
  - `type checkpoint struct{ Version int `json:"version"`; Claims map[string]checkpointClaim `json:"claims"` }`
  - `func loadCheckpoint(path string) (checkpoint, error)` — missing file → `{Version: 1, Claims: {}}`, nil; `"claims": null` → empty map
  - `func (c checkpoint) save(path string) error` — atomic
  - `func (c checkpoint) with(uid string, cl checkpointClaim) checkpoint` and `func (c checkpoint) without(uid string) checkpoint` — return copies; the receiver's map is never mutated
  - `func (c checkpoint) holder(device, port, except string) (uid string, ok bool)` — any claim other than `except` holding a device with that name or that port; when several do, the lexically smallest uid

- [ ] **Step 1: Write the failing tests**

```go
func TestLoadCheckpointMissingIsEmpty(t *testing.T)        // Version 1, len(Claims) == 0, err == nil
func TestLoadCheckpointNullClaims(t *testing.T)            // file {"version":1,"claims":null} → Claims != nil, len 0
func TestCheckpointRoundTrip(t *testing.T)
	// c := checkpoint{1, {}}.with("uid-a", {"selfhosted", "lora", [{"usb-239a-8029-fd14948954cd1f4f", "lora", "PNP0D10:02-v2-1"}]});
	// c.save(p); loadCheckpoint(p) deep-equals c; file contains `"version": 1`
func TestCheckpointSaveIsAtomic(t *testing.T)              // after save, the dir holds exactly checkpoint.json
func TestCheckpointCopyOnWrite(t *testing.T)               // c.with(...) and c.without("uid-a") leave len(c.Claims) unchanged
func TestLoadCheckpointRejectsUnknownVersion(t *testing.T) // {"version": 2, "claims": {}} → err contains "version 2" and the file path
func TestLoadCheckpointRejectsGarbage(t *testing.T)        // "not json" → err contains the file path
func TestCheckpointHolder(t *testing.T)
	// holder("usb-239a-8029-fd14948954cd1f4f", "", "uid-z") == ("uid-a", true); holder("other-name", "PNP0D10:02-v2-1", "uid-z") == ("uid-a", true);
	// holder("usb-239a-8029-fd14948954cd1f4f", "", "uid-a") == ("", false); holder("x", "y", "") == ("", false)
	// with claims "uid-a" (name N) and "uid-0" (port P): holder(N, P, "uid-a") == ("uid-0", true) and holder(N, P, "uid-0") == ("uid-a", true)
```

- [ ] **Step 2: Run to verify failure**
  Run: `go test ./cmd/usb-dra-kubeletplugin/ -run Checkpoint 2>&1 | tail -15`
  Expected: FAIL, build error `undefined: loadCheckpoint`.

- [ ] **Step 3: Implement** per the spec's Prepared state section: `os.ReadFile(filepath.Clean(path))`; `json.MarshalIndent(c, "", "  ")`; save = `os.CreateTemp(filepath.Dir(path), "checkpoint-*.tmp")`, write, `Sync`, `Close`, `os.Rename` onto `path`; on any error remove the temp file and return `errors.Join` of the original error and cleanup errors. `with`/`without` use `maps.Clone`.

- [ ] **Step 4: Verify**
  Run: `task test && golangci-lint run && go mod tidy -diff`
  Expected: all `ok`, lint exit 0, no diff.

- [ ] **Step 5: Commit** — paths: `cmd/usb-dra-kubeletplugin/checkpoint.go cmd/usb-dra-kubeletplugin/checkpoint_test.go`; message `feat(kubeletplugin): add versioned atomic checkpoint`.

---

### Task 7: Prepare with ttys, aliases, checkpoint and overlap check

Kind: rewrite

**Files:**
- Modify: `cmd/usb-dra-kubeletplugin/cdi.go`
- Modify: `cmd/usb-dra-kubeletplugin/driver.go`
- Modify: `cmd/usb-dra-kubeletplugin/main.go`
- Modify: `cmd/usb-dra-kubeletplugin/cdi_test.go`
- Modify: `cmd/usb-dra-kubeletplugin/driver_test.go`
- Create: `cmd/usb-dra-kubeletplugin/fixture_test.go`
- Create: `cmd/usb-dra-kubeletplugin/main_test.go`
- Modify: `deploy/daemonset.yaml`

**Interfaces:**
- Consumes: `usb.FindByName`, `usb.Discover`, `usb.Device.TTYs()`, `usb.Filter`, `usb.ParseExcludeClasses`, `usb.ParseInclude` (Tasks 2–4); `loadCheckpoint`, `checkpoint.save/with/without/holder` (Task 6).
- Produces:
  - `type preparedDevice struct{ DeviceName, DevNode, Serial, Port string; TTYs, Aliases, Requests []string }` (`Aliases[i]` is the container path for host `TTYs[i]`)
  - `func newDriverCore(cfg config) (*driver, error)` — builds the CDI handler and loads `filepath.Join(cfg.pluginDir, checkpointFile)`; `newDriver` calls it, then `kubeletplugin.Start`
  - `func assignAliases(devices []preparedDevice)`
  - `func parseFlags(args []string) (config, error)` — own `flag.FlagSet` (`flag.ContinueOnError`) with `klog.InitFlags(fs)`; flags `--driver-name`, `--node-name`, `--cdi-root`, `--kubelet-registrar-dir`, `--kubelet-plugin-dir`, `--sysfs-root`, `--rescan-interval` (as today), `--exclude-classes` (default `usb.DefaultExcludeClasses`), repeatable `--include`
  - `fixture_test.go` (package `main`): `loadFixture(t, name)` reading `../../internal/usb/testdata/sysfs/<name>.tsv`, and `withFile`/`without`, with exactly the contracts written in Task 3's Interfaces block
- File placement: `newDriverCore` in `driver.go`; `parseFlags` in `main.go`; `assignAliases` in `cdi.go`; `TestWriteClaimSpec*` and `TestAssignAliases` in `cdi_test.go`; `TestParseFlags*` in a new `main_test.go`; every other new test in `driver_test.go`. Existing `cdi_test.go` tests (`TestWriteClaimSpec`, `TestClaimSpecIsValidCDI`, `TestDeleteClaimSpec`, `TestQualifiedNames`) are kept with device names and env updated.
- Removes: the in-memory `prepared` map.

- [ ] **Step 1: Write the failing tests**

```go
// cdi_test.go
func TestWriteClaimSpecWithTTYs(t *testing.T)
	// device {DeviceName "usb-239a-8029-fd14948954cd1f4f", DevNode "/dev/bus/usb/005/002", Serial "FD14948954CD1F4F",
	//   TTYs ["/dev/ttyACM0"], Aliases ["/dev/dra/lora/tty0"], Requests ["lora"]}
	// spec device nodes in this order: {Path "/dev/bus/usb/005/002", HostPath same}, {"/dev/ttyACM0", "/dev/ttyACM0"},
	//   {Path "/dev/dra/lora/tty0", HostPath "/dev/ttyACM0"} (per device: USB node, then for each tty its host node then its alias);
	// every node Permissions "rw", Type "", Major 0, Minor 0, UID nil, GID nil, FileMode nil
	// env == ["USB_DEVNODE=/dev/bus/usb/005/002", "USB_SERIAL=FD14948954CD1F4F", "USB_TTY=/dev/ttyACM0"]
func TestWriteClaimSpecPortIdentityNoSerialEnv(t *testing.T) // Serial "" and no TTYs → env == ["USB_DEVNODE=/dev/bus/usb/005/002"]
func TestAssignAliases(t *testing.T)
	// Requests "lora" (A: 1 tty), "lora/primary" (B: 2 ttys), "radio" (C: 0 ttys)
	// → A.Aliases ["/dev/dra/lora/tty0"], B.Aliases ["/dev/dra/lora/tty1", "/dev/dra/lora/tty2"], C.Aliases nil

// driver_test.go — newTestDriver(t, fsys) uses newDriverCore with cdiRoot = t.TempDir() and pluginDir = a second, separate t.TempDir()
func TestPrepareRAKInjectsTTY(t *testing.T)        // talos-05, request "lora" → spec contains "/dev/ttyACM0" and "/dev/dra/lora/tty0"
func TestPrepareRejectsAdminAccess(t *testing.T)   // result.AdminAccess = new(true) → Err contains "admin access is not supported"
func TestPrepareRejectsDeviceHeldByOtherClaim(t *testing.T)
	// prepare uid-a with the RAK; prepare uid-b with the RAK → uid-b Err contains "uid-a", "uid-b", "usb-239a-8029-fd14948954cd1f4f"
func TestPrepareRejectsSamePortUnderNewName(t *testing.T)
	// 9-1 and 11-1 serial "00000001" → prepare uid-a with "usb-0bda-2838-p-pnp0d10-04-v2-1";
	// remove 11-1 files → 9-1 is now "usb-0bda-2838-00000001"; prepare uid-b with that name → Err contains "uid-a"
func TestPrepareRewritesSpecForKnownClaim(t *testing.T)
	// prepare uid-a; delete every file in cdiRoot; prepare uid-a again → exactly one spec file; CDIDeviceIDs equal across both calls
func TestPrepareResolvesNewNodeForKnownClaim(t *testing.T) // prepare uid-a; set 5-1/devnum "7"; prepare uid-a → spec contains "/dev/bus/usb/005/007"
func TestPrepareOneUnreadableDeviceDoesNotAffectOtherClaim(t *testing.T)
	// remove 9-1:1.1/bInterfaceClass; one PrepareResourceClaims call with uid-a (RAK) and uid-b ("usb-0bda-2838-93610062")
	// → uid-a prepared; uid-b Err contains "no longer attached"
func TestCheckpointSurvivesDriverRestart(t *testing.T)
	// driver 1 prepares uid-a; driver 2 = newDriverCore on the same dirs rejects uid-b for the RAK;
	// driver 2 unprepares uid-a → no spec file, checkpoint has no "uid-a"
func TestSaveFailureLeavesStateUnchanged(t *testing.T)
	// skip when os.Geteuid() == 0; prepare uid-a; chmod pluginDir 0500 (t.Cleanup restores 0700); unprepare uid-a → error; chmod 0700; driver state still holds uid-a;
	// unprepare uid-a again → nil; reload checkpoint from disk → no uid-a
func TestUnprepareUnknownClaimRemovesSpec(t *testing.T) // spec for uid-z written via cdi.writeClaimSpec only; unprepare uid-z → nil, spec gone
func TestParseFlagsAcceptsDaemonSetArgs(t *testing.T)
	// t.Setenv("NODE_NAME", "talos-05-wrk"); parseFlags([]string{"--driver-name=usb.jacaudi.dev", "--rescan-interval=30s",
	//   "-v=2", "--exclude-classes=03,08,e0"}) → err == nil
func TestParseFlagsRejectsBadValues(t *testing.T)
	// t.Setenv("NODE_NAME", "n"); "--exclude-classes=zz" → err contains "exclude-classes"; "--include=239a" → err contains "include"
```
  Existing tests: keep `TestPrepareMapsDeviceNode` (pins CDI ID format and PoolName), `TestPrepareIsIdempotent`, `TestPrepareFailsWhenDongleUnplugged` (no spec left behind), `TestPrepareIgnoresOtherDrivers`, `TestPrepareRejectsUnallocatedClaim`, `TestPrepareRejectsClaimWithNoDevicesForUs`, `TestUnprepareRemovesSpecAndIsIdempotent`, with device names updated. `TestPrepareResolvesNodeAfterReenumeration` is replaced by `TestPrepareResolvesNewNodeForKnownClaim` (it reads the removed `prepared` map).

- [ ] **Step 2: Run to verify failure**
  Run: `go test ./cmd/usb-dra-kubeletplugin/ 2>&1 | tail -15`
  Expected: FAIL (build errors: `undefined: newDriverCore`, `assignAliases`, `parseFlags`, unknown field `TTYs`).

- [ ] **Step 3: Implement** per the spec's Prepare, Unprepare and Prepared state sections. Decided here:
  - The driver holds `state checkpoint` and `checkpointPath string`; `prepareOne` and `unprepareOne` hold `d.mu` for their whole body.
  - Prepare order (resolve precedes the overlap check because the check needs each device's current port; ledger ruling B7): (1) any result for this driver with `AdminAccess != nil && *AdminAccess` → error `admin access is not supported`; (2) resolve every device with `usb.FindByName`; (3) for each, `d.state.holder(name, port, uid)` returning ok → error naming both uids and the device; (4) `assignAliases`; write the spec; (5) `next := d.state.with(uid, …)`; `next.save(path)`; on success `d.state = next`, on failure return the error (spec stays; unprepare removes it).
  - Unprepare: `cdi.deleteClaimSpec(uid)`; if `uid` is in `d.state.Claims`, `next := d.state.without(uid)`; `next.save`; on success `d.state = next`, on failure return the error with state unchanged.
  - `assignAliases` keys counters by `resourceclaim.BaseRequestRef(dev.Requests[0])`.
  - `run()` calls `parseFlags(os.Args[1:])` first, then builds clients; NODE_NAME/`--node-name` empty is an error inside `parseFlags`, checked after flag validation.
  - `deploy/daemonset.yaml` args gain `--exclude-classes=03,08,e0`.

- [ ] **Step 4: Verify**
  Run: `task test && golangci-lint run && go mod tidy -diff && yamllint deploy/`
  Expected: all `ok`, lint exit 0, no diff, yamllint exit 0.

- [ ] **Step 5: Commit** — paths: `cmd/usb-dra-kubeletplugin deploy/daemonset.yaml`; message `feat(kubeletplugin): inject ttys with aliases and persist prepared claims`.

---

### Task 8: Documentation and invariants

Kind: rewrite

**Files:**
- Modify: `README.md`
- Modify: `.claude/CLAUDE.md`
- Create: `.markdownlint-cli2.yaml`

**Interfaces:**
- Consumes: everything above (names, flags, attributes, manifests, test names).

- [ ] **Step 1: Write `.markdownlint-cli2.yaml`** with exactly:

```yaml
config:
  MD013: false
```

- [ ] **Step 2: Rewrite `README.md`** for usb-dra with these sections: What it publishes (exclusion rule, both flags, hubs/vhci skipped); Identity (serial vs port, `identity` attribute, duplicate serials fall back to port identity and stop matching serial pins); Attributes (table from the spec); Selecting devices (the CEL selector rules and the three shipped examples, spelled out in full); ttys and aliases (`/dev/dra/<request>/tty<k>`, env vars, usb-serial module note: "if autoload fails, load `ch341`/`cp210x`/`ftdi_sio`/`pl2303` via Talos `machine.kernel.modules`"); Prepared state (checkpoint path; delete the file to recover from a corrupt-checkpoint startup error); Known limitations (the spec's five items); Install (`kubectl apply -f deploy/daemonset.yaml -f deploy/deviceclasses.yaml -f deploy/claims.yaml`). Do not claim the Docker build runs tests. Table delimiter rows use spaced pipes (`| --- | --- |`) to satisfy MD060.

- [ ] **Step 3: Rewrite `.claude/CLAUDE.md`**: project name and goal for usb-dra; the Invariants section lists the spec's 12 invariants, each followed by its tests exactly as mapped here:
  1. `TestAttributesNeverExposeNodes`
  2. `TestPrepareResolvesNewNodeForKnownClaim`
  3. `TestNodeResourcesPublishesEmptyPool`
  4. `TestWriteClaimSpecWithTTYs`
  5. `TestPrepareIsIdempotent`, `TestUnprepareUnknownClaimRemovesSpec`
  6. `TestScanTalos01HubsOnly`, `TestScanSkipsVhciSilently`
  7. `TestMissingSerialIsPortIdentity`, `TestAllZeroSerialIsPortIdentity`
  8. `TestNodeResourcesCapsAt128`, `TestLongNameIsTruncatedWithHash`, `TestAttributesBoundStrings`, `TestScanSkipsOverlongPort`, `TestPortCollisionSkipsBoth`
  9. `TestScanSkipsUnreadableInterface`, `TestPrepareOneUnreadableDeviceDoesNotAffectOtherClaim`
  10. `TestClassSelectorsNeverError`, `TestSelectorsSerialPinSkipsDuplicate`
  11. `TestCheckpointSurvivesDriverRestart`, `TestSaveFailureLeavesStateUnchanged`
  12. `TestPrepareRejectsDeviceHeldByOtherClaim`, `TestPrepareRejectsSamePortUnderNewName`, `TestPrepareRejectsAdminAccess`

  Update Layout for the new files; Commands section without a test count and without the claim that `docker build` runs tests; the pin rule "`k8s.io/*` stays v0.34.x while the cluster runs 1.35; bumping is a separate change"; the Duplicate serials section rewritten to the port-identity fallback; keep Deployment gotchas and Git identity unchanged.

- [ ] **Step 4: Verify**
  Run: `for t in $(grep -oE '\bTest[A-Z][A-Za-z0-9_]+' .claude/CLAUDE.md | sort -u); do grep -rqw "func $t" --include='*_test.go' . || echo "MISSING $t"; done; npx -y markdownlint-cli2@0.24.1 README.md .claude/CLAUDE.md`
  Expected: no `MISSING` lines; `Summary: 0 issues in 0 files` (or the tool's equivalent zero-issues summary).

- [ ] **Step 5: Commit** — paths: `README.md .claude/CLAUDE.md .markdownlint-cli2.yaml`; message `docs: describe usb-dra and its invariants`.

---

### Task 9: Bump vulnerable dependencies and the Go toolchain

Kind: rewrite

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Consumes: the module state after Task 5's `go mod tidy`.
- Produces: no API change. New acceptance criterion: `govulncheck` reports no vulnerability the code calls.

- [ ] **Step 1: Confirm the RED state**
  Run: `GOTOOLCHAIN=go1.26.11 govulncheck ./... 2>&1 | grep -E 'Found in|Fixed in' | sort -u`
  Expected: `Found in` lines for `golang.org/x/net@v0.57.0` and `google.golang.org/grpc@v1.82.1` (stdlib is already clean under go1.26.11; the advisory GO-2026-6859 is fixed in go1.26.11). If `GOTOOLCHAIN=go1.26.11` cannot download the toolchain, `PLAN: BLOCKED — every path is a guess: toolchain download`.

- [ ] **Step 2: Bump**
  Run: `go get golang.org/x/net@v0.60.0 google.golang.org/grpc@v1.83.1 && go mod edit -toolchain=go1.26.11 && go mod tidy && git diff go.mod`
  Expected: `golang.org/x/net` → `v0.60.0`; `google.golang.org/grpc` → `v1.83.1`; `toolchain go1.26.11`; `go 1.26` may become `go 1.26.0`; indirect bumps limited to `golang.org/x/mod`, `golang.org/x/sys`, `golang.org/x/term`, `golang.org/x/text`, `google.golang.org/genproto/googleapis/rpc` and other `golang.org/x/*` modules. Any `k8s.io/*` or `tags.cncf.io/*` version change is `PLAN: BLOCKED — every path is a guess: dependency bump moved k8s.io`.

- [ ] **Step 3: Verify**
  Run: `task test && golangci-lint run && go mod tidy -diff && GOTOOLCHAIN=go1.26.11 govulncheck ./...; echo "vuln-exit=$?"`
  Expected: all `ok`, lint exit 0, no diff, govulncheck prints either `No vulnerabilities found.` or only the "your code doesn't appear to call these vulnerabilities" note, and `vuln-exit=0`.

- [ ] **Step 4: Commit** — paths: `go.mod go.sum`; message `chore(deps): bump x/net, grpc and the go toolchain to clear govulncheck`.

