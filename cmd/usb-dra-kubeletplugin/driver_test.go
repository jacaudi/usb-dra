package main

import (
	"context"
	"os"
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

func newTestDriver(t *testing.T, fsys fstest.MapFS) (*driver, string) {
	t.Helper()
	dir := t.TempDir()
	cdi, err := newCDIHandler(dir, usb.DriverName)
	if err != nil {
		t.Fatalf("newCDIHandler: %v", err)
	}
	return &driver{
		cdi:        cdi,
		sysfs:      fsys,
		nodeName:   "talos-01-cp",
		driverName: usb.DriverName,
		prepared:   map[types.UID][]preparedDevice{},
	}, dir
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
	return resourceapi.DeviceRequestAllocationResult{
		Request: "radio",
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

// The design decision under test: the node is resolved at PREPARE time from the
// serial, not captured when the slice was published. A dongle that re-enumerated
// onto a different bus must still map to its current node.
func TestPrepareResolvesNodeAfterReenumeration(t *testing.T) {
	// Published as bus 1 dev 5; by prepare time it has moved to bus 3 dev 11.
	d, _ := newTestDriver(t, sysfsWith([5]string{"3-1.1", "0bda", "2838", "35650102", "3/11"}))

	res := d.prepareOne(context.Background(), claimFor("uid-1", allocResult("usb-0bda-2838-35650102")))
	if res.Err != nil {
		t.Fatalf("prepare failed: %v", res.Err)
	}

	d.mu.Lock()
	prepared := d.prepared["uid-1"]
	d.mu.Unlock()
	if len(prepared) != 1 {
		t.Fatalf("got %d prepared devices, want 1", len(prepared))
	}
	if want := "/dev/bus/usb/003/011"; prepared[0].DevNode != want {
		t.Errorf("DevNode = %q, want %q (must follow re-enumeration)", prepared[0].DevNode, want)
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
