package rtlsdr

import (
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/dynamic-resource-allocation/resourceslice"
)

// DriverName is the DRA driver name. Attributes published without a domain prefix
// are implicitly qualified by it, so a claim selects on:
//
//	device.attributes["sdr.jacaudi.dev"].serial == "35650102"
const DriverName = "sdr.jacaudi.dev"

func strPtr(s string) *string { return &s }

// Attributes describes the dongle to CEL selectors in a ResourceClaim.
//
// Only stable identity is published. The device node is deliberately excluded: bus
// and device numbers change on every re-enumeration, so a claim written against them
// would silently select the wrong radio after a replug.
func (d Device) Attributes() map[resourceapi.QualifiedName]resourceapi.DeviceAttribute {
	attrs := map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
		"serial":  {StringValue: strPtr(d.Serial)},
		"vendor":  {StringValue: strPtr(d.Vendor)},
		"product": {StringValue: strPtr(d.Product)},
	}
	if d.Manufacturer != "" {
		attrs["manufacturer"] = resourceapi.DeviceAttribute{StringValue: strPtr(d.Manufacturer)}
	}
	if d.ProductName != "" {
		attrs["productName"] = resourceapi.DeviceAttribute{StringValue: strPtr(d.ProductName)}
	}
	return attrs
}

// ResourceSliceDevices converts discovered dongles into published DRA devices.
func ResourceSliceDevices(devices []Device) []resourceapi.Device {
	out := make([]resourceapi.Device, 0, len(devices))
	for _, d := range devices {
		out = append(out, resourceapi.Device{
			Name:       d.DeviceName(),
			Attributes: d.Attributes(),
		})
	}
	return out
}

// NodeResources builds the per-node pool this driver publishes.
//
// One pool per node, named after the node, because a USB dongle is only reachable
// from the machine it is plugged into. An empty slice is still published when no
// dongle is present: "pool with no devices" tells the scheduler the driver is
// healthy and simply has nothing, which is different from "no pool at all".
func NodeResources(nodeName string, devices []Device) resourceslice.DriverResources {
	return resourceslice.DriverResources{
		Pools: map[string]resourceslice.Pool{
			nodeName: {
				Slices: []resourceslice.Slice{{
					Devices: ResourceSliceDevices(devices),
				}},
			},
		},
	}
}
