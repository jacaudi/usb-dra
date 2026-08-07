package rtlsdr

import (
	"testing"

	resourceapi "k8s.io/api/resource/v1"
)

func TestAttributes(t *testing.T) {
	d := Device{
		Serial:       "35650102",
		Vendor:       "0bda",
		Product:      "2838",
		Manufacturer: "Realtek",
		ProductName:  "RTL2838UHIDIR",
		Bus:          1,
		Dev:          5,
	}

	attrs := d.Attributes()

	// The serial is the whole point: it is what a pinned claim selects on.
	serial, ok := attrs["serial"]
	if !ok {
		t.Fatal("serial attribute missing; pinned claims would be impossible")
	}
	if serial.StringValue == nil || *serial.StringValue != "35650102" {
		t.Errorf("serial = %v, want 35650102", serial.StringValue)
	}

	// The device node must NOT be published: it changes on re-enumeration, so a
	// claim written against it would select the wrong radio after a replug.
	for _, forbidden := range []resourceapi.QualifiedName{"devNode", "bus", "dev", "busPath"} {
		if _, found := attrs[forbidden]; found {
			t.Errorf("attribute %q must not be published: it is not stable", forbidden)
		}
	}

	// DRA caps attributes+capacities at 32 per device.
	if len(attrs) > 32 {
		t.Errorf("got %d attributes, DRA allows at most 32", len(attrs))
	}
}

func TestAttributesOmitsEmptyStrings(t *testing.T) {
	// Hubs and some sticks report no manufacturer/product strings. Publishing an
	// empty attribute would make `has(device.attributes...)` selectors misleading.
	d := Device{Serial: "37728719", Vendor: "0bda", Product: "2838"}

	attrs := d.Attributes()
	if _, found := attrs["manufacturer"]; found {
		t.Error("empty manufacturer must be omitted, not published as \"\"")
	}
	if _, found := attrs["productName"]; found {
		t.Error("empty productName must be omitted, not published as \"\"")
	}
}

func TestNodeResources(t *testing.T) {
	devices := []Device{
		{Serial: "35650102", Vendor: "0bda", Product: "2838", Bus: 1, Dev: 5},
		{Serial: "37728719", Vendor: "0bda", Product: "2838", Bus: 2, Dev: 5},
	}

	res := NodeResources("talos-01-cp", devices)

	pool, ok := res.Pools["talos-01-cp"]
	if !ok {
		t.Fatalf("pool must be named after the node; got pools %v", res.Pools)
	}
	if len(pool.Slices) != 1 {
		t.Fatalf("got %d slices, want 1", len(pool.Slices))
	}
	got := pool.Slices[0].Devices
	if len(got) != 2 {
		t.Fatalf("got %d devices, want 2", len(got))
	}
	if got[0].Name != "rtlsdr-35650102" {
		t.Errorf("device name = %q, want rtlsdr-35650102", got[0].Name)
	}
}

func TestNodeResourcesPublishesEmptyPool(t *testing.T) {
	// A pool with no devices means "driver healthy, no dongles", which the
	// scheduler must be able to tell apart from "driver absent".
	res := NodeResources("r230-t02", nil)

	pool, ok := res.Pools["r230-t02"]
	if !ok {
		t.Fatal("a node with no dongles must still publish a pool")
	}
	if len(pool.Slices) != 1 {
		t.Fatalf("got %d slices, want exactly 1 (empty)", len(pool.Slices))
	}
	if len(pool.Slices[0].Devices) != 0 {
		t.Errorf("got %d devices, want 0", len(pool.Slices[0].Devices))
	}
}
