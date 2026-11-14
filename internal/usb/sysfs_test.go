package usb

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

var defaultFilter = Filter{ExcludeClasses: map[string]bool{"03": true, "08": true, "e0": true}}

func candDirs(cands []candidate) []string {
	var dirs []string
	for _, c := range cands {
		dirs = append(dirs, c.Dir)
	}
	return dirs
}

func findCand(t *testing.T, cands []candidate, dir string) candidate {
	t.Helper()
	for _, c := range cands {
		if c.Dir == dir {
			return c
		}
	}
	t.Fatalf("no candidate %q in %v", dir, candDirs(cands))
	return candidate{}
}

// assertSkipsOnly scans the talos-05-wrk fixture as mutated and requires exactly the one skip, with the other default-filter devices still published.
func assertSkipsOnly(t *testing.T, mutate func(fstest.MapFS), want Skip) {
	t.Helper()
	fsys := loadFixture(t, "talos-05-wrk")
	mutate(fsys)
	cands, skips, err := scan(fsys, defaultFilter)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !reflect.DeepEqual(skips, []Skip{want}) {
		t.Errorf("skips = %+v, want %+v", skips, []Skip{want})
	}
	wantDirs := slices.DeleteFunc([]string{"11-1", "5-1", "9-1"}, func(d string) bool { return d == want.Dir })
	if got := candDirs(cands); !slices.Equal(got, wantDirs) {
		t.Errorf("candidate dirs = %v, want %v", got, wantDirs)
	}
}

func TestScanTalos05(t *testing.T) {
	cands, skips, err := scan(loadFixture(t, "talos-05-wrk"), defaultFilter)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(skips) != 0 {
		t.Errorf("skips = %+v, want none", skips)
	}
	if got, want := candDirs(cands), []string{"11-1", "5-1", "9-1"}; !slices.Equal(got, want) {
		t.Fatalf("candidate dirs = %v, want %v", got, want)
	}

	want := []candidate{
		{
			Dir: "11-1", Vendor: "0bda", Product: "2838", RawSerial: "35650102",
			Manufacturer: "Nooelec", ProductName: "NESDR SMArt v5", Port: "PNP0D10:05-v2-1",
			Interfaces: []Interface{{0, "ff", nil}}, Bus: 11, Dev: 2,
		},
		{
			Dir: "5-1", Vendor: "239a", Product: "8029", RawSerial: "FD14948954CD1F4F",
			Manufacturer: "RAKwireless", ProductName: "WisCore RAK4631 Board", Port: "PNP0D10:02-v2-1",
			Interfaces: []Interface{{0, "02", []string{"ttyACM0"}}, {1, "0a", nil}}, Bus: 5, Dev: 2,
		},
	}
	for _, w := range want {
		if got := findCand(t, cands, w.Dir); !reflect.DeepEqual(got, w) {
			t.Errorf("candidate %s = %+v, want %+v", w.Dir, got, w)
		}
	}

	nine := findCand(t, cands, "9-1")
	if nine.Port != "PNP0D10:04-v2-1" {
		t.Errorf("9-1 Port = %q, want PNP0D10:04-v2-1", nine.Port)
	}
	if want := []Interface{{0, "ff", nil}, {1, "ff", nil}}; !reflect.DeepEqual(nine.Interfaces, want) {
		t.Errorf("9-1 Interfaces = %+v, want %+v", nine.Interfaces, want)
	}
}

func TestScanTalos05NoExclusions(t *testing.T) {
	cands, skips, err := scan(loadFixture(t, "talos-05-wrk"), Filter{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(skips) != 0 {
		t.Errorf("skips = %+v, want none", skips)
	}
	if got, want := candDirs(cands), []string{"11-1", "15-1", "5-1", "9-1"}; !slices.Equal(got, want) {
		t.Fatalf("candidate dirs = %v, want %v", got, want)
	}
	mediatek := findCand(t, cands, "15-1")
	if mediatek.Port != "PNP0D10:08-v2-1" {
		t.Errorf("15-1 Port = %q, want PNP0D10:08-v2-1", mediatek.Port)
	}
	if mediatek.RawSerial != "000000000" {
		t.Errorf("15-1 RawSerial = %q, want 000000000", mediatek.RawSerial)
	}
}

func TestScanTalos01HubsOnly(t *testing.T) {
	cands, skips, err := scan(loadFixture(t, "talos-01-cp"), Filter{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(cands) != 0 || len(skips) != 0 {
		t.Errorf("cands = %v, skips = %+v, want none", candDirs(cands), skips)
	}
}

func TestScanSkipsVhciSilently(t *testing.T) {
	fsys := loadFixture(t, "talos-05-wrk")
	withFile(fsys, "17-1/idVendor", "abcd")
	withFile(fsys, "17-1/idProduct", "0001")
	withFile(fsys, "17-1/busnum", "17")
	withFile(fsys, "17-1/devnum", "2")
	withFile(fsys, "17-1/bDeviceClass", "00")
	withFile(fsys, "17-1:1.0/bInterfaceClass", "ff")
	withFile(fsys, "17-1:1.0/bInterfaceNumber", "00")

	cands, skips, err := scan(fsys, defaultFilter)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(skips) != 0 {
		t.Errorf("skips = %+v, want none", skips)
	}
	if slices.Contains(candDirs(cands), "17-1") {
		t.Errorf("vhci device 17-1 published: %v", candDirs(cands))
	}
}

func TestScanSkipsUnconfigured(t *testing.T) {
	assertSkipsOnly(t, func(fsys fstest.MapFS) {
		without(fsys, "5-1:1.0")
		without(fsys, "5-1:1.1")
	}, Skip{Dir: "5-1", Reason: "no interfaces"})
}

func TestScanSkipsUnreadableDevice(t *testing.T) {
	assertSkipsOnly(t, func(fsys fstest.MapFS) {
		withFile(fsys, "5-1/devnum", "x")
	}, Skip{Dir: "5-1", Reason: "unreadable device"})
}

func TestScanSkipsUnreadableInterface(t *testing.T) {
	assertSkipsOnly(t, func(fsys fstest.MapFS) {
		without(fsys, "5-1:1.1/bInterfaceClass")
	}, Skip{Dir: "5-1", Reason: "unreadable interface"})
}

func TestScanSkipsBadInterfaceNumber(t *testing.T) {
	assertSkipsOnly(t, func(fsys fstest.MapFS) {
		withFile(fsys, "5-1:1.1/bInterfaceNumber", "zz")
	}, Skip{Dir: "5-1", Reason: "unreadable interface"})
}

func TestScanSkipsUnknownSpeed(t *testing.T) {
	assertSkipsOnly(t, func(fsys fstest.MapFS) {
		withFile(fsys, "usb5/speed", "unknown")
	}, Skip{Dir: "5-1", Reason: "unknown root hub speed"})
}

func TestScanSkipsUnreadableRootHub(t *testing.T) {
	assertSkipsOnly(t, func(fsys fstest.MapFS) {
		without(fsys, "usb5/serial")
	}, Skip{Dir: "5-1", Reason: "unreadable root hub"})
}

func TestScanSkipsOverlongPort(t *testing.T) {
	assertSkipsOnly(t, func(fsys fstest.MapFS) {
		withFile(fsys, "usb5/serial", strings.Repeat("a", 70))
	}, Skip{Dir: "5-1", Reason: "port longer than 64 bytes", Severe: true})
}

func TestScanMissingRootIsError(t *testing.T) {
	if _, _, err := scan(fstest.MapFS{}, Filter{}); err == nil {
		t.Error("scan of an empty filesystem succeeded, want an error")
	}
}

func TestScanOrdersInterfacesAndTTYsNumerically(t *testing.T) {
	fsys := fstest.MapFS{}
	withFile(fsys, "usb3/serial", "0000:00:14.0")
	withFile(fsys, "usb3/speed", "480")
	withFile(fsys, "usb3/bDeviceClass", "09")
	withFile(fsys, "3-1/idVendor", "1a86")
	withFile(fsys, "3-1/idProduct", "7523")
	withFile(fsys, "3-1/busnum", "3")
	withFile(fsys, "3-1/devnum", "4")
	withFile(fsys, "3-1/bDeviceClass", "00")
	withFile(fsys, "3-1:1.10/bInterfaceNumber", "0a")
	withFile(fsys, "3-1:1.10/bInterfaceClass", "ff")
	withFile(fsys, "3-1:1.10/ttyUSB10/dev", "188:10")
	withFile(fsys, "3-1:1.10/ttyUSB2/dev", "188:2")
	withFile(fsys, "3-1:1.2/bInterfaceNumber", "02")
	withFile(fsys, "3-1:1.2/bInterfaceClass", "02")
	withFile(fsys, "3-1:1.2/tty/ttyACM3/dev", "166:3")

	cands, skips, err := scan(fsys, Filter{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(skips) != 0 {
		t.Fatalf("skips = %+v, want none", skips)
	}
	got := findCand(t, cands, "3-1")
	want := []Interface{
		{2, "02", []string{"ttyACM3"}},
		{10, "ff", []string{"ttyUSB2", "ttyUSB10"}},
	}
	if !reflect.DeepEqual(got.Interfaces, want) {
		t.Errorf("Interfaces = %+v, want %+v", got.Interfaces, want)
	}
	if got.Port != "0000:00:14.0-v2-1" {
		t.Errorf("Port = %q, want 0000:00:14.0-v2-1", got.Port)
	}
}

func TestPortRevision(t *testing.T) {
	tests := []struct {
		speed  string
		want   string
		wantOK bool
	}{
		{"1.5", "1", true},
		{"12", "1", true},
		{"480", "2", true},
		{"5000", "3", true},
		{"10000", "3", true},
		{"20000", "3", true},
		{"unknown", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.speed, func(t *testing.T) {
			got, ok := portRevision(tt.speed)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("portRevision(%q) = (%q, %t), want (%q, %t)", tt.speed, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
