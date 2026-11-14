package usb

import (
	"cmp"
	"fmt"
	"io/fs"
	"math"
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode"

	resourceapi "k8s.io/api/resource/v1"
)

const (
	reasonUnreadableDevice    = "unreadable device"
	reasonNoInterfaces        = "no interfaces"
	reasonUnreadableInterface = "unreadable interface"
	reasonUnreadableRootHub   = "unreadable root hub"
	reasonUnknownSpeed        = "unknown root hub speed"
)

// hubClass is the bDeviceClass of root hubs and external hubs.
const hubClass = "09"

// Interface is one USB interface of a device.
type Interface struct {
	Number int      // bInterfaceNumber
	Class  string   // bInterfaceClass, two lowercase hex digits
	TTYs   []string // kernel tty names bound to the interface, in numeric order
}

// candidate is a device that passed the hub, vhci and class filters, with the sysfs data a published device needs.
type candidate struct {
	Dir          string // device directory name, e.g. "5-1"
	Vendor       string // idVendor, lowercased
	Product      string // idProduct, lowercased
	RawSerial    string // trimmed sysfs serial, "" when absent
	Manufacturer string // trimmed sysfs manufacturer, "" when absent
	ProductName  string // trimmed sysfs product, "" when absent
	Port         string // <controller>-v<revision>-<chain>
	Interfaces   []Interface
	Bus, Dev     int
}

// Skip records a device the scan could not publish; Severe marks a skip an operator must act on.
type Skip struct {
	Dir, Reason string
	Severe      bool
}

// scan lists the publishable USB devices under SysfsUSBDir, sorted by directory name; only an unreadable SysfsUSBDir is an error.
func scan(fsys fs.FS, f Filter) ([]candidate, []Skip, error) {
	entries, err := fs.ReadDir(fsys, SysfsUSBDir)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", SysfsUSBDir, err)
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}

	var cands []candidate
	var skips []Skip
	for _, name := range names {
		if strings.Contains(name, ":") {
			continue
		}
		c, skip, ok := readCandidate(fsys, f, name, names)
		switch {
		case ok:
			cands = append(cands, c)
		case skip != nil:
			skips = append(skips, *skip)
		}
	}
	return cands, skips, nil
}

// readCandidate reads one device directory; ok=false with a nil skip drops the device silently (hub, vhci, filtered out, or a vanished identity file).
func readCandidate(fsys fs.FS, f Filter, name string, names []string) (candidate, *Skip, bool) {
	dir := path.Join(SysfsUSBDir, name)
	ids := make([]string, 4)
	for i, attr := range []string{"idVendor", "idProduct", "busnum", "devnum"} {
		v, err := readAttr(fsys, dir, attr)
		if err != nil {
			return candidate{}, nil, false
		}
		ids[i] = v
	}
	bus, busErr := strconv.Atoi(ids[2])
	dev, devErr := strconv.Atoi(ids[3])
	class, classErr := readAttr(fsys, dir, "bDeviceClass")
	if busErr != nil || devErr != nil || classErr != nil {
		return candidate{}, &Skip{Dir: name, Reason: reasonUnreadableDevice}, false
	}
	if class == hubClass {
		return candidate{}, nil, false
	}

	root := path.Join(SysfsUSBDir, "usb"+strconv.Itoa(bus))
	controller, ctrlErr := readAttr(fsys, root, "serial")
	if ctrlErr == nil && strings.HasPrefix(controller, "vhci_hcd") {
		return candidate{}, nil, false
	}

	intfs, reason := readInterfaces(fsys, name, names)
	if reason != "" {
		return candidate{}, &Skip{Dir: name, Reason: reason}, false
	}

	c := candidate{
		Dir:        name,
		Vendor:     strings.ToLower(ids[0]),
		Product:    strings.ToLower(ids[1]),
		Interfaces: intfs,
		Bus:        bus,
		Dev:        dev,
	}
	c.RawSerial, _ = readAttr(fsys, dir, "serial")
	c.Manufacturer, _ = readAttr(fsys, dir, "manufacturer")
	c.ProductName, _ = readAttr(fsys, dir, "product")

	classes := make([]string, len(intfs))
	for i, in := range intfs {
		classes[i] = in.Class
	}
	if !f.Publish(c.Vendor, c.Product, c.RawSerial, classes) {
		return candidate{}, nil, false
	}

	var skip *Skip
	if c.Port, skip = portOf(fsys, name, bus, root, controller, ctrlErr); skip != nil {
		return candidate{}, skip, false
	}
	return c, nil, true
}

// readInterfaces reads the interface directories of device name, ordered by interface number; reason is "" on success.
func readInterfaces(fsys fs.FS, name string, names []string) (intfs []Interface, reason string) {
	for _, n := range names {
		if !strings.HasPrefix(n, name+":") {
			continue
		}
		in, ok := readInterface(fsys, path.Join(SysfsUSBDir, n))
		if !ok {
			return nil, reasonUnreadableInterface
		}
		intfs = append(intfs, in)
	}
	if len(intfs) == 0 {
		return nil, reasonNoInterfaces
	}
	slices.SortFunc(intfs, func(a, b Interface) int { return cmp.Compare(a.Number, b.Number) })
	return intfs, ""
}

func readInterface(fsys fs.FS, dir string) (Interface, bool) {
	num, err := readHexAttr(fsys, dir, "bInterfaceNumber")
	if err != nil {
		return Interface{}, false
	}
	class, err := readHexAttr(fsys, dir, "bInterfaceClass")
	if err != nil {
		return Interface{}, false
	}
	return Interface{Number: num, Class: fmt.Sprintf("%02x", class), TTYs: readTTYs(fsys, dir)}, true
}

// readHexAttr parses a sysfs attribute printed as one or two hex digits.
func readHexAttr(fsys fs.FS, dir, name string) (int, error) {
	s, err := readAttr(fsys, dir, name)
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseUint(s, 16, 8)
	if err != nil || v > math.MaxUint8 {
		return 0, fmt.Errorf("parse %s %q: want one byte in hex", name, s)
	}
	return int(v), nil
}

// readTTYs lists the ttys of an interface: <dir>/tty/<name> (cdc_acm) and <dir>/ttyUSB<n> (usb-serial); an unreadable directory has none.
func readTTYs(fsys fs.FS, dir string) []string {
	var ttys []string
	if es, err := fs.ReadDir(fsys, path.Join(dir, "tty")); err == nil {
		for _, e := range es {
			ttys = append(ttys, e.Name())
		}
	}
	if es, err := fs.ReadDir(fsys, dir); err == nil {
		for _, e := range es {
			if strings.HasPrefix(e.Name(), "ttyUSB") {
				ttys = append(ttys, e.Name())
			}
		}
	}
	slices.SortFunc(ttys, func(a, b string) int {
		return cmp.Or(cmp.Compare(ttyNumber(a), ttyNumber(b)), cmp.Compare(a, b))
	})
	return ttys
}

// ttyNumber returns the integer after a tty name's trailing letters, 0 when there is none.
func ttyNumber(name string) int {
	n, _ := strconv.Atoi(strings.TrimLeftFunc(name, unicode.IsLetter))
	return n
}

// portOf builds <controller>-v<revision>-<chain> from the root hub's controller and speed; a non-nil skip means the device is not publishable.
func portOf(fsys fs.FS, name string, bus int, root, controller string, ctrlErr error) (string, *Skip) {
	if ctrlErr != nil {
		return "", &Skip{Dir: name, Reason: reasonUnreadableRootHub}
	}
	speed, err := readAttr(fsys, root, "speed")
	if err != nil {
		return "", &Skip{Dir: name, Reason: reasonUnreadableRootHub}
	}
	rev, known := portRevision(speed)
	if !known {
		return "", &Skip{Dir: name, Reason: reasonUnknownSpeed}
	}
	chain := strings.TrimPrefix(name, strconv.Itoa(bus)+"-")
	port := fmt.Sprintf("%s-v%s-%s", controller, rev, chain)
	if len(port) > resourceapi.DeviceAttributeMaxValueLength {
		reason := fmt.Sprintf("port longer than %d bytes", resourceapi.DeviceAttributeMaxValueLength)
		return "", &Skip{Dir: name, Reason: reason, Severe: true}
	}
	return port, nil
}

// portRevision maps a root hub speed in Mb/s to the USB revision digit of its port, as udev's path_id does.
func portRevision(speed string) (string, bool) {
	switch speed {
	case "1.5", "12":
		return "1", true
	case "480":
		return "2", true
	case "5000", "10000", "20000":
		return "3", true
	}
	return "", false
}
