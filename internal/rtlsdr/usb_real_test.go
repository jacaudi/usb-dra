package rtlsdr

import (
	"strings"
	"testing"
	"testing/fstest"
)

// realSysfs is a verbatim capture of /sys/bus/usb/devices from talos-01-cp
// (Dell R730, two Nooelec NESDR SMArt v5 dongles). Fields: dir|vendor|product|serial|bus|dev.
//
// It is kept as a regression fixture because it contains every shape that has to be
// rejected: interface directories with no attributes at all, an Intel hub with an
// EMPTY serial, a Dell hub carrying a plausible-looking serial, and root hubs whose
// "serial" is actually a PCI address.
const realSysfs = `
1-0:1.0|||||
1-1|8087|800a||1|2
1-1.4|0bda|2838|35650102|1|5
1-1.4:1.0|||||
1-1.6|413c|a001|0123456789|1|4
1-1.6:1.0|||||
1-1:1.0|||||
2-0:1.0|||||
2-1|8087|8002||2|2
2-1.2|0bda|2838|37728719|2|5
2-1.2:1.0|||||
2-1.2:1.1|||||
2-1:1.0|||||
usb1|1d6b|0002|0000:00:1a.0|1|1
usb2|1d6b|0002|0000:00:1d.0|2|1
`

// buildSysfs turns the capture above back into a filesystem, writing only the
// attribute files that were actually present on the real node.
func buildSysfs(t *testing.T, capture string) fstest.MapFS {
	t.Helper()
	fsys := fstest.MapFS{}
	for line := range strings.SplitSeq(strings.TrimSpace(capture), "\n") {
		f := strings.Split(line, "|")
		if len(f) != 6 {
			t.Fatalf("malformed fixture line %q", line)
		}
		dir, vendor, product, serial, busnum, devnum := f[0], f[1], f[2], f[3], f[4], f[5]
		base := SysfsUSBDir + "/" + dir

		// An entry with no vendor had no attribute files at all on the real node.
		if vendor == "" {
			fsys[base+"/.exists"] = &fstest.MapFile{}
			continue
		}
		fsys[base+"/idVendor"] = &fstest.MapFile{Data: []byte(vendor + "\n")}
		fsys[base+"/idProduct"] = &fstest.MapFile{Data: []byte(product + "\n")}
		fsys[base+"/busnum"] = &fstest.MapFile{Data: []byte(busnum + "\n")}
		fsys[base+"/devnum"] = &fstest.MapFile{Data: []byte(devnum + "\n")}
		// The Intel hubs expose an empty serial file rather than none at all.
		fsys[base+"/serial"] = &fstest.MapFile{Data: []byte(serial + "\n")}
	}
	return fsys
}

func TestDiscoverAgainstRealNode(t *testing.T) {
	got, dupes, err := Discover(buildSysfs(t, realSysfs))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(dupes) != 0 {
		t.Errorf("unexpected duplicate serials: %v", dupes)
	}

	want := []struct{ serial, node string }{
		{"35650102", "/dev/bus/usb/001/005"},
		{"37728719", "/dev/bus/usb/002/005"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d dongles, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Serial != w.serial {
			t.Errorf("device %d serial = %q, want %q", i, got[i].Serial, w.serial)
		}
		if got[i].DevNode() != w.node {
			t.Errorf("device %d node = %q, want %q", i, got[i].DevNode(), w.node)
		}
		if got[i].Vendor != "0bda" || got[i].Product != "2838" {
			t.Errorf("device %d = %s:%s, want 0bda:2838", i, got[i].Vendor, got[i].Product)
		}
	}
}
