package usb

import (
	"fmt"
	"maps"
	"strings"
	"testing"
	"testing/fstest"

	resourceapi "k8s.io/api/resource/v1"
)

// rakAttrs returns the string attributes of the RAK device after mutate; a non-string attribute fails the test.
func rakAttrs(t *testing.T, mutate func(fstest.MapFS)) map[string]string {
	t.Helper()
	got := map[string]string{}
	for k, v := range discoverRAK(t, mutate).Attributes() {
		if v.StringValue == nil {
			t.Fatalf("attribute %q is not a string: %+v", k, v)
		}
		got[string(k)] = *v.StringValue
	}
	return got
}

func TestAttributesRAK(t *testing.T) {
	got := rakAttrs(t, func(fstest.MapFS) {})
	want := map[string]string{
		"vendor":           "239a",
		"product":          "8029",
		"identity":         "serial",
		"port":             "PNP0D10:02-v2-1",
		"serial":           "FD14948954CD1F4F",
		"manufacturer":     "RAKwireless",
		"productName":      "WisCore RAK4631 Board",
		"interfaceClasses": "02,0a",
	}
	if !maps.Equal(got, want) {
		t.Errorf("attributes = %v, want %v", got, want)
	}
}

func TestAttributesPortIdentityHasNoSerial(t *testing.T) {
	scan, err := Discover(loadFixture(t, "talos-05-wrk"), Filter{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	attrs := findDevice(t, scan.Devices, "usb-0e8d-c616-p-pnp0d10-08-v2-1").Attributes()
	if _, ok := attrs["serial"]; ok {
		t.Error("a device without a usable serial must not publish a serial attribute")
	}
	if id := attrs["identity"].StringValue; id == nil || *id != "port" {
		t.Errorf("identity = %v, want port", id)
	}
}

func TestAttributesBoundStrings(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(fstest.MapFS)
		key    string
		want   string // "" means the key must be absent
	}{
		{"long manufacturer is cut", func(f fstest.MapFS) { withFile(f, "5-1/manufacturer", strings.Repeat("a", 70)) },
			"manufacturer", strings.Repeat("a", 64)},
		{"cut lands on a rune boundary", func(f fstest.MapFS) { withFile(f, "5-1/manufacturer", strings.Repeat("a", 63)+"é") },
			"manufacturer", strings.Repeat("a", 63)},
		{"invalid utf-8 manufacturer is dropped", func(f fstest.MapFS) { withFile(f, "5-1/manufacturer", "\xff\xfe") },
			"manufacturer", ""},
		{"invalid utf-8 product is dropped", func(f fstest.MapFS) { withFile(f, "5-1/product", "\xff\xfe") },
			"productName", ""},
		{"overlong interfaceClasses is dropped", manyInterfaces, "interfaceClasses", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := rakAttrs(t, tt.mutate)[tt.key]
			if tt.want == "" {
				if ok {
					t.Errorf("%s = %q, want the key absent", tt.key, got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("%s = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

// manyInterfaces gives the RAK device 22 interfaces of distinct classes, whose joined list is 65 bytes.
func manyInterfaces(fsys fstest.MapFS) {
	for n := 2; n < 22; n++ {
		dir := fmt.Sprintf("5-1:1.%d", n)
		withFile(fsys, dir+"/bInterfaceNumber", fmt.Sprintf("%02x", n))
		withFile(fsys, dir+"/bInterfaceClass", fmt.Sprintf("%02x", 0x20+n))
	}
}

func TestAttributesNeverExposeNodes(t *testing.T) {
	forbidden := []string{"devnode", "bus", "busnum", "devnum", "tty", "ttys"}
	for key := range rakAttrs(t, func(fstest.MapFS) {}) {
		for _, bad := range forbidden {
			if key == bad {
				t.Errorf("attribute %q must not be published: it is not stable", key)
			}
		}
	}
}

func syntheticDevices(n int) []Device {
	devs := make([]Device, n)
	for i := range devs {
		devs[i] = Device{Name: fmt.Sprintf("usb-0000-0000-%03d", i), Identity: IdentityPort, Vendor: "0000", Product: "0000", Port: "x"}
	}
	return devs
}

func TestNodeResourcesCapsAt128(t *testing.T) {
	devs := syntheticDevices(130)

	res, dropped := NodeResources("talos-05-wrk", devs)

	pool, ok := res.Pools["talos-05-wrk"]
	if !ok || len(pool.Slices) != 1 {
		t.Fatalf("want one pool with one slice, got %+v", res.Pools)
	}
	got := pool.Slices[0].Devices
	if len(got) != resourceapi.ResourceSliceMaxDevices {
		t.Fatalf("got %d devices, want %d", len(got), resourceapi.ResourceSliceMaxDevices)
	}
	if dropped != 2 {
		t.Errorf("dropped = %d, want 2", dropped)
	}
	if last := got[len(got)-1].Name; last != devs[127].Name {
		t.Errorf("last published = %q, want %q (the first 128 are kept)", last, devs[127].Name)
	}
}

func TestNodeResourcesPublishesEmptyPool(t *testing.T) {
	// A pool with no devices means "driver healthy, nothing attached", which the scheduler must tell apart from "driver absent".
	res, dropped := NodeResources("r230-t02", nil)

	pool, ok := res.Pools["r230-t02"]
	if !ok {
		t.Fatal("a node with no devices must still publish a pool")
	}
	if len(pool.Slices) != 1 {
		t.Fatalf("got %d slices, want exactly 1 (empty)", len(pool.Slices))
	}
	if len(pool.Slices[0].Devices) != 0 {
		t.Errorf("got %d devices, want 0", len(pool.Slices[0].Devices))
	}
	if dropped != 0 {
		t.Errorf("dropped = %d, want 0", dropped)
	}
}
