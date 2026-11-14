// Package usb discovers USB devices from sysfs and models them as DRA devices.
package usb

import (
	"bytes"
	"fmt"
	"io/fs"
	"path"
	"slices"
)

// SysfsUSBDir is the sysfs path holding one directory per USB device, relative to
// the filesystem root. It is a relative path so it can be opened from an fs.FS.
const SysfsUSBDir = "sys/bus/usb/devices"

// Identity says what a device's name is derived from.
type Identity string

const (
	// IdentitySerial names a device by its usable, unique serial; the name follows the device across ports.
	IdentitySerial Identity = "serial"
	// IdentityPort names a device by its controller and port chain; the name follows the port, not the device.
	IdentityPort Identity = "port"
)

// Device is one published USB device.
type Device struct {
	Name         string // ResourceSlice device name, a DNS label
	Identity     Identity
	Vendor       string // idVendor, lowercase hex
	Product      string // idProduct, lowercase hex
	Serial       string // raw serial when usable, kept even when duplicated; "" otherwise
	Port         string // <controller>-v<revision>-<chain>
	Manufacturer string // "" when absent or not valid UTF-8, else at most 64 bytes
	ProductName  string // same bounds as Manufacturer
	Interfaces   []Interface
	Bus, Dev     int
}

// DevNode returns the character device libusb opens; it is never published as an attribute because bus and device numbers change on re-enumeration.
func (d Device) DevNode() string {
	return fmt.Sprintf("/dev/bus/usb/%03d/%03d", d.Bus, d.Dev)
}

// InterfaceClasses returns the sorted, unique interface classes of the device.
func (d Device) InterfaceClasses() []string {
	classes := make([]string, len(d.Interfaces))
	for i, in := range d.Interfaces {
		classes[i] = in.Class
	}
	slices.Sort(classes)
	return slices.Compact(classes)
}

// TTYs returns the /dev paths of the device's ttys, in interface order then tty order.
func (d Device) TTYs() []string {
	var ttys []string
	for _, in := range d.Interfaces {
		for _, tty := range in.TTYs {
			ttys = append(ttys, "/dev/"+tty)
		}
	}
	return ttys
}

// Scan is the result of one sysfs scan.
type Scan struct {
	Devices          []Device // sorted by Name, not capped
	DuplicateSerials []string // raw serials shared by several devices, sorted and unique
	Skipped          []Skip
}

// Discover lists every device the filter publishes, named and made unique; only an unreadable SysfsUSBDir is an error.
func Discover(fsys fs.FS, f Filter) (Scan, error) {
	cands, skips, err := scan(fsys, f)
	if err != nil {
		return Scan{}, err
	}
	devices, duplicates, collisions := nameDevices(cands)
	return Scan{Devices: devices, DuplicateSerials: duplicates, Skipped: append(skips, collisions...)}, nil
}

func readAttr(fsys fs.FS, dir, name string) (string, error) {
	b, err := fs.ReadFile(fsys, path.Join(dir, name))
	if err != nil {
		return "", err
	}
	return string(bytes.TrimSpace(b)), nil
}
