package usb

import (
	"fmt"
	"slices"
	"strings"
)

// DefaultExcludeClasses lists the interface classes (HID, mass storage, wireless controller) the driver hides by default.
const DefaultExcludeClasses = "03,08,e0"

// Include re-admits a device that the class filter would otherwise hide; an empty Serial matches any serial.
type Include struct{ Vendor, Product, Serial string }

// Filter decides which devices are published; the zero Filter publishes everything.
type Filter struct {
	ExcludeClasses map[string]bool
	Includes       []Include
}

// ParseExcludeClasses parses a comma-separated list of two-digit lowercase hex interface classes; "" yields an empty set.
func ParseExcludeClasses(s string) (map[string]bool, error) {
	classes := map[string]bool{}
	if s == "" {
		return classes, nil
	}
	for tok := range strings.SplitSeq(s, ",") {
		if !isLowerHex(tok, 2) {
			return nil, fmt.Errorf("parse exclude classes %q: token %q must be two lowercase hex digits", s, tok)
		}
		classes[tok] = true
	}
	return classes, nil
}

// ParseInclude parses "vid:pid" or "vid:pid:serial"; the serial is everything after the second colon.
func ParseInclude(s string) (Include, error) {
	parts := strings.SplitN(s, ":", 3)
	if len(parts) < 2 {
		return Include{}, fmt.Errorf("parse include %q: want vid:pid or vid:pid:serial", s)
	}
	if !isLowerHex(parts[0], 4) {
		return Include{}, fmt.Errorf("parse include %q: vendor %q must be four lowercase hex digits", s, parts[0])
	}
	if !isLowerHex(parts[1], 4) {
		return Include{}, fmt.Errorf("parse include %q: product %q must be four lowercase hex digits", s, parts[1])
	}
	inc := Include{Vendor: parts[0], Product: parts[1]}
	if len(parts) == 3 {
		if parts[2] == "" {
			return Include{}, fmt.Errorf("parse include %q: serial must not be empty", s)
		}
		inc.Serial = parts[2]
	}
	return inc, nil
}

// Publish reports whether a device is offered to claims; rawSerial is the trimmed sysfs serial, "" when absent.
func (f Filter) Publish(vendor, product, rawSerial string, interfaceClasses []string) bool {
	for _, inc := range f.Includes {
		if inc.Vendor == vendor && inc.Product == product && (inc.Serial == "" || strings.EqualFold(inc.Serial, rawSerial)) {
			return true
		}
	}
	if len(interfaceClasses) == 0 {
		return true
	}
	return slices.ContainsFunc(interfaceClasses, func(c string) bool { return !f.ExcludeClasses[c] })
}

// isLowerHex reports whether s is exactly n lowercase hex digits.
func isLowerHex(s string, n int) bool {
	return len(s) == n && !strings.ContainsFunc(s, func(r rune) bool {
		return (r < '0' || r > '9') && (r < 'a' || r > 'f')
	})
}
