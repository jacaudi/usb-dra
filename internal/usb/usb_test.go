package usb

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

func deviceNames(devs []Device) []string {
	names := make([]string, len(devs))
	for i, d := range devs {
		names[i] = d.Name
	}
	return names
}

func findDevice(t *testing.T, devs []Device, name string) Device {
	t.Helper()
	for _, d := range devs {
		if d.Name == name {
			return d
		}
	}
	t.Fatalf("no device %q in %v", name, deviceNames(devs))
	return Device{}
}

// discoverRAK runs Discover on talos-05-wrk after mutate and returns the RAK device, which must be published.
func discoverRAK(t *testing.T, mutate func(fstest.MapFS)) Device {
	t.Helper()
	fsys := loadFixture(t, "talos-05-wrk")
	mutate(fsys)
	scan, err := Discover(fsys, defaultFilter)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	for _, d := range scan.Devices {
		if d.Vendor == "239a" {
			return d
		}
	}
	t.Fatalf("RAK device missing from %v (skipped %+v)", deviceNames(scan.Devices), scan.Skipped)
	return Device{}
}

func assertPortIdentity(t *testing.T, d Device, wantName string) {
	t.Helper()
	if d.Identity != IdentityPort {
		t.Errorf("Identity = %q, want %q", d.Identity, IdentityPort)
	}
	if d.Serial != "" {
		t.Errorf("Serial = %q, want empty", d.Serial)
	}
	if d.Name != wantName {
		t.Errorf("Name = %q, want %q", d.Name, wantName)
	}
}

func TestDiscoverTalos05(t *testing.T) {
	scan, err := Discover(loadFixture(t, "talos-05-wrk"), defaultFilter)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	want := []string{"usb-0bda-2838-35650102", "usb-0bda-2838-93610062", "usb-239a-8029-fd14948954cd1f4f"}
	if got := deviceNames(scan.Devices); !slices.Equal(got, want) {
		t.Fatalf("names = %v, want %v", got, want)
	}

	rak := findDevice(t, scan.Devices, "usb-239a-8029-fd14948954cd1f4f")
	if rak.Identity != IdentitySerial {
		t.Errorf("RAK Identity = %q, want %q", rak.Identity, IdentitySerial)
	}
	if got, want := rak.DevNode(), "/dev/bus/usb/005/002"; got != want {
		t.Errorf("RAK DevNode = %q, want %q", got, want)
	}
	if got, want := rak.TTYs(), []string{"/dev/ttyACM0"}; !slices.Equal(got, want) {
		t.Errorf("RAK TTYs = %v, want %v", got, want)
	}

	nooelec := findDevice(t, scan.Devices, "usb-0bda-2838-93610062")
	if got, want := nooelec.InterfaceClasses(), []string{"ff"}; !slices.Equal(got, want) {
		t.Errorf("InterfaceClasses = %v, want %v", got, want)
	}
	if len(scan.DuplicateSerials) != 0 {
		t.Errorf("DuplicateSerials = %v, want none", scan.DuplicateSerials)
	}
}

func TestDiscoverMediaTekPortIdentity(t *testing.T) {
	scan, err := Discover(loadFixture(t, "talos-05-wrk"), Filter{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	const name = "usb-0e8d-c616-p-pnp0d10-08-v2-1"
	assertPortIdentity(t, findDevice(t, scan.Devices, name), name)
}

func TestDuplicateSerialsFallBackToPort(t *testing.T) {
	fsys := loadFixture(t, "talos-05-wrk")
	withFile(fsys, "9-1/serial", "00000001")
	withFile(fsys, "11-1/serial", "00000001")

	scan, err := Discover(fsys, defaultFilter)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	for _, name := range []string{"usb-0bda-2838-p-pnp0d10-04-v2-1", "usb-0bda-2838-p-pnp0d10-05-v2-1"} {
		d := findDevice(t, scan.Devices, name)
		if d.Identity != IdentityPort {
			t.Errorf("%s Identity = %q, want %q", name, d.Identity, IdentityPort)
		}
		if d.Serial != "00000001" {
			t.Errorf("%s Serial = %q, want the duplicate serial kept", name, d.Serial)
		}
	}
	if want := []string{"00000001"}; !slices.Equal(scan.DuplicateSerials, want) {
		t.Errorf("DuplicateSerials = %v, want %v", scan.DuplicateSerials, want)
	}
}

func TestCaseFoldedSerialsCollide(t *testing.T) {
	fsys := loadFixture(t, "talos-05-wrk")
	withFile(fsys, "9-1/serial", "AbC")
	withFile(fsys, "11-1/serial", "abc")

	scan, err := Discover(fsys, defaultFilter)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	for _, d := range scan.Devices {
		if d.Vendor == "0bda" && d.Identity != IdentityPort {
			t.Errorf("%s Identity = %q, want %q", d.Name, d.Identity, IdentityPort)
		}
	}
	if want := []string{"AbC", "abc"}; !slices.Equal(scan.DuplicateSerials, want) {
		t.Errorf("DuplicateSerials = %v, want %v", scan.DuplicateSerials, want)
	}
}

func TestPortCollisionSkipsBoth(t *testing.T) {
	fsys := loadFixture(t, "talos-05-wrk")
	for _, bus := range []string{"3", "4"} {
		withFile(fsys, "usb"+bus+"/serial", "0000:00:14.0")
		withFile(fsys, "usb"+bus+"/speed", "480")
		withFile(fsys, "usb"+bus+"/bDeviceClass", "09")
		dir := bus + "-1"
		withFile(fsys, dir+"/idVendor", "1a86")
		withFile(fsys, dir+"/idProduct", "7523")
		withFile(fsys, dir+"/busnum", bus)
		withFile(fsys, dir+"/devnum", "2")
		withFile(fsys, dir+"/bDeviceClass", "00")
		withFile(fsys, dir+":1.0/bInterfaceNumber", "00")
		withFile(fsys, dir+":1.0/bInterfaceClass", "ff")
	}

	scan, err := Discover(fsys, defaultFilter)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	for _, d := range scan.Devices {
		if d.Vendor == "1a86" {
			t.Errorf("colliding device %s was published", d.Name)
		}
	}
	want := []Skip{
		{Dir: "3-1", Reason: "name collision", Severe: true},
		{Dir: "4-1", Reason: "name collision", Severe: true},
	}
	if !slices.Equal(scan.Skipped, want) {
		t.Errorf("Skipped = %+v, want %+v", scan.Skipped, want)
	}
}

func TestMissingSerialIsPortIdentity(t *testing.T) {
	d := discoverRAK(t, func(fsys fstest.MapFS) { without(fsys, "5-1/serial") })
	assertPortIdentity(t, d, "usb-239a-8029-p-pnp0d10-02-v2-1")
}

func TestAllZeroSerialIsPortIdentity(t *testing.T) {
	d := discoverRAK(t, func(fsys fstest.MapFS) { withFile(fsys, "5-1/serial", "000000000") })
	assertPortIdentity(t, d, "usb-239a-8029-p-pnp0d10-02-v2-1")
}

func TestOverlongSerialIsPortIdentity(t *testing.T) {
	d := discoverRAK(t, func(fsys fstest.MapFS) { withFile(fsys, "5-1/serial", strings.Repeat("A", 65)) })
	assertPortIdentity(t, d, "usb-239a-8029-p-pnp0d10-02-v2-1")
}

func TestInvalidUTF8SerialIsPortIdentity(t *testing.T) {
	d := discoverRAK(t, func(fsys fstest.MapFS) { withFile(fsys, "5-1/serial", "\xff\xfe") })
	assertPortIdentity(t, d, "usb-239a-8029-p-pnp0d10-02-v2-1")
}

func TestControlCharSerialIsPortIdentity(t *testing.T) {
	for _, serial := range []string{"AB\x07CD", "AB\x1bCD", "AB\x7fCD"} {
		d := discoverRAK(t, func(fsys fstest.MapFS) { withFile(fsys, "5-1/serial", serial) })
		assertPortIdentity(t, d, "usb-239a-8029-p-pnp0d10-02-v2-1")
	}
}
