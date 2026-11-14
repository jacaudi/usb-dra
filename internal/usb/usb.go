// Package usb discovers USB devices from sysfs and models them as DRA devices.
package usb

import (
	"bytes"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

// SysfsUSBDir is the sysfs path holding one directory per USB device, relative to
// the filesystem root. It is a relative path so it can be opened from an fs.FS.
const SysfsUSBDir = "sys/bus/usb/devices"

// supported lists the USB IDs of RTL2832U-based dongles. Realtek's 0bda:2838 covers
// the RTL2838UHIDIR that every Nooelec/RTL-SDR Blog v3 and v4 stick reports.
var supported = map[string]map[string]bool{
	"0bda": {"2838": true, "2832": true},
}

// Device is one physical dongle.
type Device struct {
	Serial       string // EEPROM serial; the stable identity a claim can select on
	Vendor       string // USB idVendor, e.g. "0bda"
	Product      string // USB idProduct, e.g. "2838"
	Manufacturer string // USB manufacturer string, may be empty
	ProductName  string // USB product string, may be empty
	Bus          int    // USB bus number
	Dev          int    // USB device number on that bus
}

// DevNode returns the character device libusb opens for this dongle.
//
// This is deliberately not published as a ResourceSlice attribute: the bus/device
// numbers change every time the dongle re-enumerates, so it is resolved fresh at
// prepare time by looking the serial up again.
func (d Device) DevNode() string {
	return fmt.Sprintf("/dev/bus/usb/%03d/%03d", d.Bus, d.Dev)
}

// DeviceName is the ResourceSlice device name for this dongle. Device names must be
// DNS labels, so the serial is lowercased; RTL-SDR serials are alphanumeric.
func (d Device) DeviceName() string {
	return "rtlsdr-" + strings.ToLower(d.Serial)
}

// Discover enumerates every supported dongle visible under sysfs, sorted by serial
// so that published ResourceSlices are stable between calls.
//
// Devices whose serial is missing or blank are skipped: without a serial there is
// nothing durable to select on, and a claim pinned to one would be unsatisfiable.
// A dongle still carrying the factory-default serial is reported via DuplicateSerials.
func Discover(fsys fs.FS) (devices []Device, duplicates []string, err error) {
	entries, err := fs.ReadDir(fsys, SysfsUSBDir)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", SysfsUSBDir, err)
	}

	seen := map[string]int{}
	for _, e := range entries {
		// Interface directories look like "1-1:1.0" and carry no idVendor.
		if strings.Contains(e.Name(), ":") {
			continue
		}
		dev, ok, err := readDevice(fsys, path.Join(SysfsUSBDir, e.Name()))
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			continue
		}
		seen[dev.Serial]++
		devices = append(devices, dev)
	}

	sort.Slice(devices, func(i, j int) bool { return devices[i].Serial < devices[j].Serial })

	for serial, n := range seen {
		if n > 1 {
			duplicates = append(duplicates, serial)
		}
	}
	sort.Strings(duplicates)

	return devices, duplicates, nil
}

// readDevice parses one sysfs USB device directory. It reports ok=false for
// directories that are not a supported dongle, or that carry no usable serial.
func readDevice(fsys fs.FS, dir string) (Device, bool, error) {
	vendor, err := readAttr(fsys, dir, "idVendor")
	if err != nil {
		// Hubs, root devices and partially-populated directories are not an error.
		return Device{}, false, nil
	}
	product, err := readAttr(fsys, dir, "idProduct")
	if err != nil {
		return Device{}, false, nil
	}
	if !supported[strings.ToLower(vendor)][strings.ToLower(product)] {
		return Device{}, false, nil
	}

	serial, err := readAttr(fsys, dir, "serial")
	if err != nil || serial == "" {
		return Device{}, false, nil
	}

	bus, err := readIntAttr(fsys, dir, "busnum")
	if err != nil {
		return Device{}, false, fmt.Errorf("%s: %w", dir, err)
	}
	devnum, err := readIntAttr(fsys, dir, "devnum")
	if err != nil {
		return Device{}, false, fmt.Errorf("%s: %w", dir, err)
	}

	manufacturer, _ := readAttr(fsys, dir, "manufacturer")
	productName, _ := readAttr(fsys, dir, "product")

	return Device{
		Serial:       serial,
		Vendor:       strings.ToLower(vendor),
		Product:      strings.ToLower(product),
		Manufacturer: manufacturer,
		ProductName:  productName,
		Bus:          bus,
		Dev:          devnum,
	}, true, nil
}

func readAttr(fsys fs.FS, dir, name string) (string, error) {
	b, err := fs.ReadFile(fsys, path.Join(dir, name))
	if err != nil {
		return "", err
	}
	return string(bytes.TrimSpace(b)), nil
}

func readIntAttr(fsys fs.FS, dir, name string) (int, error) {
	s, err := readAttr(fsys, dir, name)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", name, err)
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("parse %s %q: %w", name, s, err)
	}
	return v, nil
}

// FindBySerial resolves a dongle's current device node by serial. Prepare uses this
// rather than a cached path so a dongle that re-enumerated between publish and
// prepare is still mapped to the right node.
func FindBySerial(fsys fs.FS, serial string) (Device, error) {
	devices, _, err := Discover(fsys)
	if err != nil {
		return Device{}, err
	}
	for _, d := range devices {
		if strings.EqualFold(d.Serial, serial) {
			return d, nil
		}
	}
	return Device{}, fmt.Errorf("no RTL-SDR with serial %q is attached", serial)
}
