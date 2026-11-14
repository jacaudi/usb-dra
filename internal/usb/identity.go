package usb

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	resourceapi "k8s.io/api/resource/v1"
)

const reasonNameCollision = "name collision"

const (
	maxNameLen  = 63 // DNS-1123 label limit
	hashLen     = 8
	keptNameLen = maxNameLen - 1 - hashLen // the rest of a truncated name is "-" and the hash
)

// deviceName builds the ResourceSlice name usb-<vendor>-<product>-<id>, reduced to a DNS label; a name over 63 characters keeps its first 54 and ends in a hash of the whole.
func deviceName(vendor, product, id string) string {
	name := strings.Join(strings.FieldsFunc(strings.ToLower("usb-"+vendor+"-"+product+"-"+id), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	}), "-")
	if len(name) <= maxNameLen {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	return name[:keptNameLen] + "-" + hex.EncodeToString(sum[:])[:hashLen]
}

// usableSerial returns raw when it can identify a device (non-empty, valid UTF-8, short enough, no control characters, not all zeros), else "".
func usableSerial(raw string) string {
	if raw == "" || len(raw) > resourceapi.DeviceAttributeMaxValueLength || !utf8.ValidString(raw) || strings.ContainsFunc(raw, unicode.IsControl) || strings.Trim(raw, "0") == "" {
		return ""
	}
	return raw
}

// boundString returns s cut on a rune boundary to the attribute length limit, or "" when s is not valid UTF-8.
func boundString(s string) string {
	if !utf8.ValidString(s) {
		return ""
	}
	n := min(len(s), resourceapi.DeviceAttributeMaxValueLength)
	for n < len(s) && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// newDevice builds the device for c, named by its serial when usable and by its port otherwise.
func newDevice(c candidate) Device {
	d := Device{
		Identity:     IdentityPort,
		Vendor:       c.Vendor,
		Product:      c.Product,
		Serial:       usableSerial(c.RawSerial),
		Port:         c.Port,
		Manufacturer: boundString(c.Manufacturer),
		ProductName:  boundString(c.ProductName),
		Interfaces:   c.Interfaces,
		Bus:          c.Bus,
		Dev:          c.Dev,
	}
	if d.Serial != "" {
		d.Identity = IdentitySerial
	}
	d.Name = identityName(d)
	return d
}

// identityName names d from its current identity.
func identityName(d Device) string {
	if d.Identity == IdentitySerial {
		return deviceName(d.Vendor, d.Product, d.Serial)
	}
	return deviceName(d.Vendor, d.Product, "p-"+d.Port)
}

// nameDevices names candidates uniquely and sorts them by name: shared serial names fall back to port identity (serials returned as duplicates), and names that still collide are dropped as severe skips.
func nameDevices(cands []candidate) (devices []Device, duplicates []string, collisions []Skip) {
	all := make([]Device, len(cands))
	for i, c := range cands {
		all[i] = newDevice(c)
	}
	duplicates = demoteDuplicates(all)

	counts := map[string]int{}
	for _, d := range all {
		counts[d.Name]++
	}
	for i, d := range all {
		if counts[d.Name] > 1 {
			collisions = append(collisions, Skip{Dir: cands[i].Dir, Reason: reasonNameCollision, Severe: true})
			continue
		}
		devices = append(devices, d)
	}
	slices.SortFunc(devices, func(a, b Device) int { return strings.Compare(a.Name, b.Name) })
	return devices, duplicates, collisions
}

// demoteDuplicates switches every serial-identity device whose name is shared to port identity and returns the sorted unique raw serials involved.
func demoteDuplicates(devs []Device) []string {
	counts := map[string]int{}
	for _, d := range devs {
		if d.Identity == IdentitySerial {
			counts[d.Name]++
		}
	}
	var duplicates []string
	for i, d := range devs {
		if d.Identity == IdentitySerial && counts[d.Name] > 1 {
			duplicates = append(duplicates, d.Serial)
			devs[i].Identity = IdentityPort
			devs[i].Name = identityName(devs[i])
		}
	}
	slices.Sort(duplicates)
	return slices.Compact(duplicates)
}
