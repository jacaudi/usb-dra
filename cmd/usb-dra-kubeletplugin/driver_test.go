package main

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"

	"github.com/jacaudi/usb-dra/internal/usb"
)

// sysfsWith builds a fake sysfs containing the given dongles, each with one vendor-class interface and a root hub for its bus.
func sysfsWith(devs ...[5]string) fstest.MapFS {
	fsys := fstest.MapFS{}
	put := func(rel, content string) {
		fsys[usb.SysfsUSBDir+"/"+rel] = &fstest.MapFile{Data: []byte(content)}
	}
	for _, d := range devs {
		dir, vendor, product, serial, busdev := d[0], d[1], d[2], d[3], d[4]
		bus, dev, _ := strings.Cut(busdev, "/")
		put(dir+"/idVendor", vendor)
		put(dir+"/idProduct", product)
		put(dir+"/serial", serial)
		put(dir+"/busnum", bus)
		put(dir+"/devnum", dev)
		put(dir+"/bDeviceClass", "00")
		put(dir+":1.0/bInterfaceClass", "ff")
		put(dir+":1.0/bInterfaceNumber", "00")
		put("usb"+bus+"/serial", "0000:00:14.0")
		put("usb"+bus+"/speed", "480")
		put("usb"+bus+"/bDeviceClass", "09")
	}
	return fsys
}

const (
	rakName = "usb-239a-8029-fd14948954cd1f4f"
	rtlName = "usb-0bda-2838-93610062"
)

// newDriverIn builds a driver over fsys whose CDI specs and checkpoint live in the given directories.
func newDriverIn(t *testing.T, fsys fstest.MapFS, cdiRoot, pluginDir string) *driver {
	t.Helper()
	exclude, err := usb.ParseExcludeClasses(usb.DefaultExcludeClasses)
	if err != nil {
		t.Fatalf("ParseExcludeClasses: %v", err)
	}
	d, err := newDriverCore(config{
		driverName: usb.DriverName,
		nodeName:   "talos-01-cp",
		cdiRoot:    cdiRoot,
		pluginDir:  pluginDir,
		sysfs:      fsys,
		filter:     usb.Filter{ExcludeClasses: exclude},
	})
	if err != nil {
		t.Fatalf("newDriverCore: %v", err)
	}
	return d
}

// newTestDriver returns a driver with fresh directories and its CDI spec directory.
func newTestDriver(t *testing.T, fsys fstest.MapFS) (*driver, string) {
	t.Helper()
	cdiRoot := t.TempDir()
	return newDriverIn(t, fsys, cdiRoot, t.TempDir()), cdiRoot
}

// readSpecs returns the concatenated text of every file in dir.
func readSpecs(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var sb strings.Builder
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read spec: %v", err)
		}
		sb.Write(raw)
	}
	return sb.String()
}

func unprepareRef(uid string) kubeletplugin.NamespacedObject {
	return kubeletplugin.NamespacedObject{
		NamespacedName: types.NamespacedName{Namespace: "selfhosted", Name: "radio"},
		UID:            types.UID(uid),
	}
}

func claimFor(uid string, results ...resourceapi.DeviceRequestAllocationResult) *resourceapi.ResourceClaim {
	return &resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "radio", Namespace: "selfhosted", UID: types.UID(uid)},
		Status: resourceapi.ResourceClaimStatus{
			Allocation: &resourceapi.AllocationResult{
				Devices: resourceapi.DeviceAllocationResult{Results: results},
			},
		},
	}
}

func allocResult(device string) resourceapi.DeviceRequestAllocationResult {
	return allocResultFor("radio", device)
}

func allocResultFor(request, device string) resourceapi.DeviceRequestAllocationResult {
	return resourceapi.DeviceRequestAllocationResult{
		Request: request,
		Driver:  usb.DriverName,
		Pool:    "talos-01-cp",
		Device:  device,
	}
}

func TestPrepareMapsDeviceNode(t *testing.T) {
	d, _ := newTestDriver(t, sysfsWith([5]string{"1-1.4", "0bda", "2838", "35650102", "1/5"}))

	res := d.prepareOne(context.Background(), claimFor("uid-1", allocResult("usb-0bda-2838-35650102")))
	if res.Err != nil {
		t.Fatalf("prepare failed: %v", res.Err)
	}
	if len(res.Devices) != 1 {
		t.Fatalf("got %d devices, want 1", len(res.Devices))
	}

	got := res.Devices[0]
	if got.DeviceName != "usb-0bda-2838-35650102" {
		t.Errorf("DeviceName = %q", got.DeviceName)
	}
	if got.PoolName != "talos-01-cp" {
		t.Errorf("PoolName = %q, want the node name", got.PoolName)
	}
	if len(got.Requests) != 1 || got.Requests[0] != "radio" {
		t.Errorf("Requests = %v, want [radio]", got.Requests)
	}
	want := "k8s.usb.jacaudi.dev/usb=uid-1-usb-0bda-2838-35650102"
	if len(got.CDIDeviceIDs) != 1 || got.CDIDeviceIDs[0] != want {
		t.Errorf("CDIDeviceIDs = %v, want [%s]", got.CDIDeviceIDs, want)
	}
}

func TestPrepareIsIdempotent(t *testing.T) {
	d, dir := newTestDriver(t, sysfsWith([5]string{"1-1.4", "0bda", "2838", "35650102", "1/5"}))
	claim := claimFor("uid-1", allocResult("usb-0bda-2838-35650102"))

	first := d.prepareOne(context.Background(), claim)
	if first.Err != nil {
		t.Fatalf("first prepare: %v", first.Err)
	}
	second := d.prepareOne(context.Background(), claim)
	if second.Err != nil {
		t.Fatalf("second prepare (kubelet retry) failed: %v", second.Err)
	}
	if len(second.Devices) != 1 || second.Devices[0].CDIDeviceIDs[0] != first.Devices[0].CDIDeviceIDs[0] {
		t.Errorf("retry returned different devices: %v vs %v", second.Devices, first.Devices)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("got %d spec files, want 1 (retry must not duplicate)", len(entries))
	}
}

func TestPrepareFailsWhenDongleUnplugged(t *testing.T) {
	// Allocated by the scheduler, but physically gone before the pod started.
	d, dir := newTestDriver(t, sysfsWith([5]string{"1-1.4", "0bda", "2838", "37728719", "1/5"}))

	res := d.prepareOne(context.Background(), claimFor("uid-1", allocResult("usb-0bda-2838-35650102")))
	if res.Err == nil {
		t.Fatal("want an error when the allocated dongle is absent, got nil")
	}
	if !strings.Contains(res.Err.Error(), "no longer attached") {
		t.Errorf("error should say the device is gone, got: %v", res.Err)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("a failed prepare must not leave a CDI spec behind: %v", entries)
	}
}

func TestPrepareIgnoresOtherDrivers(t *testing.T) {
	d, _ := newTestDriver(t, sysfsWith([5]string{"1-1.4", "0bda", "2838", "35650102", "1/5"}))

	foreign := allocResult("gpu-0")
	foreign.Driver = "gpu.intel.com"
	claim := claimFor("uid-1", foreign, allocResult("usb-0bda-2838-35650102"))

	res := d.prepareOne(context.Background(), claim)
	if res.Err != nil {
		t.Fatalf("prepare failed: %v", res.Err)
	}
	if len(res.Devices) != 1 || res.Devices[0].DeviceName != "usb-0bda-2838-35650102" {
		t.Errorf("must prepare only this driver's devices, got %v", res.Devices)
	}
}

func TestPrepareRejectsUnallocatedClaim(t *testing.T) {
	d, _ := newTestDriver(t, sysfsWith([5]string{"1-1.4", "0bda", "2838", "35650102", "1/5"}))

	claim := claimFor("uid-1")
	claim.Status.Allocation = nil

	if res := d.prepareOne(context.Background(), claim); res.Err == nil {
		t.Fatal("want an error for an unallocated claim, got nil")
	}
}

func TestPrepareRejectsClaimWithNoDevicesForUs(t *testing.T) {
	d, _ := newTestDriver(t, sysfsWith([5]string{"1-1.4", "0bda", "2838", "35650102", "1/5"}))

	foreign := allocResult("gpu-0")
	foreign.Driver = "gpu.intel.com"

	if res := d.prepareOne(context.Background(), claimFor("uid-1", foreign)); res.Err == nil {
		t.Fatal("want an error when no device belongs to this driver, got nil")
	}
}

func TestUnprepareRemovesSpecAndIsIdempotent(t *testing.T) {
	d, dir := newTestDriver(t, sysfsWith([5]string{"1-1.4", "0bda", "2838", "35650102", "1/5"}))

	if res := d.prepareOne(context.Background(), claimFor("uid-1", allocResult("usb-0bda-2838-35650102"))); res.Err != nil {
		t.Fatalf("prepare: %v", res.Err)
	}

	claim := kubeletplugin.NamespacedObject{
		NamespacedName: types.NamespacedName{Namespace: "selfhosted", Name: "radio"},
		UID:            types.UID("uid-1"),
	}
	if err := d.unprepareOne(context.Background(), claim); err != nil {
		t.Fatalf("unprepare: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("CDI spec survived unprepare: %v", entries)
	}

	// The kubelet may call unprepare again after a restart.
	if err := d.unprepareOne(context.Background(), claim); err != nil {
		t.Errorf("second unprepare must be a no-op, got: %v", err)
	}
}

func prepareOK(t *testing.T, d *driver, uid string, results ...resourceapi.DeviceRequestAllocationResult) kubeletplugin.PrepareResult {
	t.Helper()
	res := d.prepareOne(t.Context(), claimFor(uid, results...))
	if res.Err != nil {
		t.Fatalf("prepare %s: %v", uid, res.Err)
	}
	return res
}

func TestPrepareRAKInjectsTTY(t *testing.T) {
	d, cdiRoot := newTestDriver(t, loadFixture(t, "talos-05-wrk"))
	prepareOK(t, d, "uid-a", allocResultFor("lora", rakName))

	spec := readSpecs(t, cdiRoot)
	for _, want := range []string{"/dev/ttyACM0", "/dev/dra/lora/tty0", "/dev/bus/usb/005/002"} {
		if !strings.Contains(spec, want) {
			t.Errorf("spec is missing %q:\n%s", want, spec)
		}
	}
}

func TestPrepareRejectsAdminAccess(t *testing.T) {
	d, cdiRoot := newTestDriver(t, loadFixture(t, "talos-05-wrk"))

	result := allocResultFor("lora", rakName)
	result.AdminAccess = new(true)
	res := d.prepareOne(t.Context(), claimFor("uid-a", result))
	if res.Err == nil || !strings.Contains(res.Err.Error(), "admin access is not supported") {
		t.Fatalf("Err = %v, want admin access rejection", res.Err)
	}
	if entries, _ := os.ReadDir(cdiRoot); len(entries) != 0 {
		t.Errorf("a rejected claim must not leave a CDI spec: %v", entries)
	}
}

func TestPrepareRejectsDeviceHeldByOtherClaim(t *testing.T) {
	d, cdiRoot := newTestDriver(t, loadFixture(t, "talos-05-wrk"))
	prepareOK(t, d, "uid-a", allocResultFor("lora", rakName))

	res := d.prepareOne(t.Context(), claimFor("uid-b", allocResultFor("lora", rakName)))
	if res.Err == nil {
		t.Fatal("want an error for a device another claim holds, got nil")
	}
	for _, want := range []string{"uid-a", "uid-b", rakName} {
		if !strings.Contains(res.Err.Error(), want) {
			t.Errorf("Err = %q, want it to name %q", res.Err, want)
		}
	}
	if entries, _ := os.ReadDir(cdiRoot); len(entries) != 1 {
		t.Errorf("got %d spec files, want only uid-a's", len(entries))
	}
}

func TestPrepareRejectsSamePortUnderNewName(t *testing.T) {
	fsys := loadFixture(t, "talos-05-wrk")
	withFile(fsys, "9-1/serial", "00000001")
	withFile(fsys, "11-1/serial", "00000001")
	d, _ := newTestDriver(t, fsys)

	// A shared serial makes both sticks port-identity devices.
	prepareOK(t, d, "uid-a", allocResult("usb-0bda-2838-p-pnp0d10-04-v2-1"))

	// Unplugging the twin turns 9-1 back into a serial-identity device on the same port.
	without(fsys, "11-1")
	res := d.prepareOne(t.Context(), claimFor("uid-b", allocResult("usb-0bda-2838-00000001")))
	if res.Err == nil || !strings.Contains(res.Err.Error(), "uid-a") {
		t.Fatalf("Err = %v, want rejection naming uid-a", res.Err)
	}
}

func TestPrepareRewritesSpecForKnownClaim(t *testing.T) {
	d, cdiRoot := newTestDriver(t, loadFixture(t, "talos-05-wrk"))
	first := prepareOK(t, d, "uid-a", allocResultFor("lora", rakName))

	// A reboot empties the tmpfs CDI directory while the checkpoint survives.
	entries, err := os.ReadDir(cdiRoot)
	if err != nil {
		t.Fatalf("read cdi root: %v", err)
	}
	for _, e := range entries {
		if err := os.Remove(filepath.Join(cdiRoot, e.Name())); err != nil {
			t.Fatalf("remove spec: %v", err)
		}
	}

	second := prepareOK(t, d, "uid-a", allocResultFor("lora", rakName))
	if entries, _ := os.ReadDir(cdiRoot); len(entries) != 1 {
		t.Errorf("got %d spec files, want 1 rewritten spec", len(entries))
	}
	if !slices.Equal(first.Devices[0].CDIDeviceIDs, second.Devices[0].CDIDeviceIDs) {
		t.Errorf("CDI IDs changed across prepares: %v vs %v", first.Devices[0].CDIDeviceIDs, second.Devices[0].CDIDeviceIDs)
	}
}

func TestPrepareResolvesNewNodeForKnownClaim(t *testing.T) {
	fsys := loadFixture(t, "talos-05-wrk")
	d, cdiRoot := newTestDriver(t, fsys)
	prepareOK(t, d, "uid-a", allocResultFor("lora", rakName))

	withFile(fsys, "5-1/devnum", "7")
	prepareOK(t, d, "uid-a", allocResultFor("lora", rakName))

	if spec := readSpecs(t, cdiRoot); !strings.Contains(spec, "/dev/bus/usb/005/007") {
		t.Errorf("spec must follow re-enumeration to devnum 7:\n%s", spec)
	}
}

func TestPrepareOneUnreadableDeviceDoesNotAffectOtherClaim(t *testing.T) {
	fsys := loadFixture(t, "talos-05-wrk")
	without(fsys, "9-1:1.1/bInterfaceClass")
	d, _ := newTestDriver(t, fsys)

	results, err := d.PrepareResourceClaims(t.Context(), []*resourceapi.ResourceClaim{
		claimFor("uid-a", allocResultFor("lora", rakName)),
		claimFor("uid-b", allocResultFor("radio", rtlName)),
	})
	if err != nil {
		t.Fatalf("PrepareResourceClaims: %v", err)
	}
	if got := results["uid-a"]; got.Err != nil {
		t.Errorf("uid-a must prepare, got %v", got.Err)
	}
	if got := results["uid-b"]; got.Err == nil || !strings.Contains(got.Err.Error(), "no longer attached") {
		t.Errorf("uid-b Err = %v, want no longer attached", got.Err)
	}
}

func TestCheckpointSurvivesDriverRestart(t *testing.T) {
	fsys := loadFixture(t, "talos-05-wrk")
	cdiRoot, pluginDir := t.TempDir(), t.TempDir()
	prepareOK(t, newDriverIn(t, fsys, cdiRoot, pluginDir), "uid-a", allocResultFor("lora", rakName))

	restarted := newDriverIn(t, fsys, cdiRoot, pluginDir)
	if res := restarted.prepareOne(t.Context(), claimFor("uid-b", allocResultFor("lora", rakName))); res.Err == nil {
		t.Fatal("a restarted driver must still reject a second claim for the RAK")
	}

	if err := restarted.unprepareOne(t.Context(), unprepareRef("uid-a")); err != nil {
		t.Fatalf("unprepare: %v", err)
	}
	if entries, _ := os.ReadDir(cdiRoot); len(entries) != 0 {
		t.Errorf("CDI spec survived unprepare: %v", entries)
	}
	raw, err := os.ReadFile(filepath.Join(pluginDir, checkpointFile))
	if err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	if strings.Contains(string(raw), "uid-a") {
		t.Errorf("checkpoint still names uid-a:\n%s", raw)
	}
}

func TestSaveFailureLeavesStateUnchanged(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	d, _ := newTestDriver(t, loadFixture(t, "talos-05-wrk"))
	pluginDir := filepath.Dir(d.checkpointPath)
	prepareOK(t, d, "uid-a", allocResultFor("lora", rakName))

	if err := os.Chmod(pluginDir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(pluginDir, 0o700) })
	if err := d.unprepareOne(t.Context(), unprepareRef("uid-a")); err == nil {
		t.Fatal("unprepare must fail when the checkpoint cannot be saved")
	}
	if err := os.Chmod(pluginDir, 0o700); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	if _, ok := d.state.Claims["uid-a"]; !ok {
		t.Fatal("a failed save must leave uid-a in the driver state")
	}
	if err := d.unprepareOne(t.Context(), unprepareRef("uid-a")); err != nil {
		t.Fatalf("second unprepare: %v", err)
	}
	onDisk, err := loadCheckpoint(d.checkpointPath)
	if err != nil {
		t.Fatalf("loadCheckpoint: %v", err)
	}
	if _, ok := onDisk.Claims["uid-a"]; ok {
		t.Error("checkpoint on disk still holds uid-a")
	}
}

func TestUnprepareUnknownClaimRemovesSpec(t *testing.T) {
	d, cdiRoot := newTestDriver(t, loadFixture(t, "talos-05-wrk"))
	if err := d.cdi.writeClaimSpec("uid-z", []preparedDevice{{DeviceName: rakName, DevNode: "/dev/bus/usb/005/002"}}); err != nil {
		t.Fatalf("writeClaimSpec: %v", err)
	}

	if err := d.unprepareOne(t.Context(), unprepareRef("uid-z")); err != nil {
		t.Fatalf("unprepare: %v", err)
	}
	if entries, _ := os.ReadDir(cdiRoot); len(entries) != 0 {
		t.Errorf("spec of an unknown claim survived unprepare: %v", entries)
	}
}

// scanCountingFS counts how many times the USB device directory is listed.
type scanCountingFS struct {
	fstest.MapFS
	scans int
}

func (f *scanCountingFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == usb.SysfsUSBDir {
		f.scans++
	}
	return f.MapFS.ReadDir(name)
}

func TestPrepareScansOncePerClaim(t *testing.T) {
	fsys := loadFixture(t, "talos-05-wrk")
	d, _ := newTestDriver(t, fsys)
	counting := &scanCountingFS{MapFS: fsys}
	d.sysfs = counting

	prepareOK(t, d, "uid-a",
		allocResultFor("lora", rakName),
		allocResultFor("radio", rtlName),
		allocResultFor("spare", "usb-0bda-2838-35650102"),
	)

	if counting.scans != 1 {
		t.Errorf("sysfs scans = %d, want 1 per claim", counting.scans)
	}
}

func TestPrepareAfterPortSwap(t *testing.T) {
	fsys := loadFixture(t, "talos-05-wrk")
	d, _ := newTestDriver(t, fsys)
	const otherRTL = "usb-0bda-2838-35650102"
	prepareOK(t, d, "uid-a", allocResult(rtlName))
	prepareOK(t, d, "uid-b", allocResult(otherRTL))

	// Swapping the sticks between their ports moves each serial to the other's port.
	withFile(fsys, "9-1/serial", "35650102")
	withFile(fsys, "11-1/serial", "93610062")

	prepareOK(t, d, "uid-a", allocResult(rtlName))
	prepareOK(t, d, "uid-b", allocResult(otherRTL))
}

func TestPrepareOnlyServesThisNodesPool(t *testing.T) {
	d, cdiRoot := newTestDriver(t, loadFixture(t, "talos-05-wrk"))

	result := allocResultFor("lora", rakName)
	result.Pool = "other-node"
	res := d.prepareOne(t.Context(), claimFor("uid-a", result))
	if res.Err == nil || !strings.Contains(res.Err.Error(), "other-node") {
		t.Fatalf("Err = %v, want a rejection naming the pool", res.Err)
	}
	if entries, _ := os.ReadDir(cdiRoot); len(entries) != 0 {
		t.Errorf("a rejected claim must not leave a CDI spec: %v", entries)
	}
}
