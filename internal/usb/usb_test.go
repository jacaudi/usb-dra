package usb

import (
	"maps"
	"testing"
	"testing/fstest"
)

// usbDev builds the sysfs attribute files a USB device directory exposes.
func usbDev(dir, vendor, product, serial, busnum, devnum string) fstest.MapFS {
	base := SysfsUSBDir + "/" + dir
	fsys := fstest.MapFS{
		base + "/idVendor":  {Data: []byte(vendor + "\n")},
		base + "/idProduct": {Data: []byte(product + "\n")},
		base + "/busnum":    {Data: []byte(busnum + "\n")},
		base + "/devnum":    {Data: []byte(devnum + "\n")},
	}
	if serial != "" {
		fsys[base+"/serial"] = &fstest.MapFile{Data: []byte(serial + "\n")}
	}
	return fsys
}

func merge(sets ...fstest.MapFS) fstest.MapFS {
	out := fstest.MapFS{}
	for _, s := range sets {
		maps.Copy(out, s)
	}
	return out
}

func TestDiscover(t *testing.T) {
	t.Run("finds supported dongles sorted by serial", func(t *testing.T) {
		fsys := merge(
			usbDev("1-1.4", "0bda", "2838", "93610062", "1", "5"),
			usbDev("2-1.2", "0bda", "2838", "35650102", "2", "9"),
		)

		got, dupes, err := Discover(fsys)
		if err != nil {
			t.Fatalf("Discover: %v", err)
		}
		if len(dupes) != 0 {
			t.Errorf("unexpected duplicates: %v", dupes)
		}
		if len(got) != 2 {
			t.Fatalf("got %d devices, want 2", len(got))
		}
		if got[0].Serial != "35650102" || got[1].Serial != "93610062" {
			t.Errorf("not sorted by serial: %q, %q", got[0].Serial, got[1].Serial)
		}
		if want := "/dev/bus/usb/002/009"; got[0].DevNode() != want {
			t.Errorf("DevNode = %q, want %q", got[0].DevNode(), want)
		}
		if want := "rtlsdr-35650102"; got[0].DeviceName() != want {
			t.Errorf("DeviceName = %q, want %q", got[0].DeviceName(), want)
		}
	})

	t.Run("ignores interface directories", func(t *testing.T) {
		// "1-1:1.0" is an interface, not a device, and carries no idVendor.
		fsys := merge(
			usbDev("1-1", "0bda", "2838", "35650102", "1", "5"),
			usbDev("1-1:1.0", "0bda", "2838", "35650102", "1", "5"),
		)

		got, dupes, err := Discover(fsys)
		if err != nil {
			t.Fatalf("Discover: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d devices, want 1 (interface dir must be skipped)", len(got))
		}
		if len(dupes) != 0 {
			t.Errorf("interface dir must not register as a duplicate serial: %v", dupes)
		}
	})

	t.Run("ignores unsupported vendors and hubs", func(t *testing.T) {
		fsys := merge(
			usbDev("1-1", "0bda", "2838", "35650102", "1", "5"),
			usbDev("1-2", "8087", "8002", "0000", "1", "2"), // Intel rate-matching hub
			usbDev("1-3", "046d", "c52b", "abc", "1", "3"),  // unrelated peripheral
		)

		got, _, err := Discover(fsys)
		if err != nil {
			t.Fatalf("Discover: %v", err)
		}
		if len(got) != 1 || got[0].Serial != "35650102" {
			t.Fatalf("got %+v, want only the RTL-SDR", got)
		}
	})

	t.Run("skips dongles with no serial", func(t *testing.T) {
		// Without a serial there is no durable identity to select on.
		fsys := usbDev("1-1", "0bda", "2838", "", "1", "5")

		got, _, err := Discover(fsys)
		if err != nil {
			t.Fatalf("Discover: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("got %d devices, want 0", len(got))
		}
	})

	t.Run("reports factory-default duplicate serials", func(t *testing.T) {
		// Two un-reprogrammed sticks both answer 00000001; a claim pinned to that
		// serial would be ambiguous, so the driver must be able to warn.
		fsys := merge(
			usbDev("1-1", "0bda", "2838", "00000001", "1", "5"),
			usbDev("2-1", "0bda", "2838", "00000001", "2", "6"),
		)

		got, dupes, err := Discover(fsys)
		if err != nil {
			t.Fatalf("Discover: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d devices, want 2", len(got))
		}
		if len(dupes) != 1 || dupes[0] != "00000001" {
			t.Errorf("duplicates = %v, want [00000001]", dupes)
		}
	})

	t.Run("empty sysfs yields no devices", func(t *testing.T) {
		fsys := fstest.MapFS{SysfsUSBDir + "/.keep": {Data: []byte{}}}

		got, _, err := Discover(fsys)
		if err != nil {
			t.Fatalf("Discover: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("got %d devices, want 0", len(got))
		}
	})
}

func TestFindBySerial(t *testing.T) {
	fsys := merge(
		usbDev("1-1.4", "0bda", "2838", "93610062", "1", "5"),
		usbDev("2-1.2", "0bda", "2838", "35650102", "2", "9"),
	)

	t.Run("resolves the current node for a serial", func(t *testing.T) {
		got, err := FindBySerial(fsys, "93610062")
		if err != nil {
			t.Fatalf("FindBySerial: %v", err)
		}
		if want := "/dev/bus/usb/001/005"; got.DevNode() != want {
			t.Errorf("DevNode = %q, want %q", got.DevNode(), want)
		}
	})

	t.Run("errors when the dongle is gone", func(t *testing.T) {
		if _, err := FindBySerial(fsys, "deadbeef"); err == nil {
			t.Fatal("want error for an absent serial, got nil")
		}
	})
}
