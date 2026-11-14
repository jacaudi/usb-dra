package usb

import (
	"strings"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/dynamic-resource-allocation/resourceslice"
)

// DriverName is the DRA driver name; attributes published without a domain are implicitly qualified by it.
const DriverName = "usb.jacaudi.dev"

// Attributes describes the device to CEL selectors; the device node, bus number and tty names are never published because they change on re-enumeration.
func (d Device) Attributes() map[resourceapi.QualifiedName]resourceapi.DeviceAttribute {
	attrs := map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
		"vendor":   {StringValue: new(d.Vendor)},
		"product":  {StringValue: new(d.Product)},
		"identity": {StringValue: new(string(d.Identity))},
		"port":     {StringValue: new(d.Port)},
	}
	optional := func(key resourceapi.QualifiedName, value string) {
		if value != "" {
			attrs[key] = resourceapi.DeviceAttribute{StringValue: new(value)}
		}
	}
	optional("serial", d.Serial)
	optional("manufacturer", d.Manufacturer)
	optional("productName", d.ProductName)
	if classes := strings.Join(d.InterfaceClasses(), ","); len(classes) <= resourceapi.DeviceAttributeMaxValueLength {
		optional("interfaceClasses", classes)
	}
	return attrs
}

// NodeResources builds this node's pool, always with one slice even when empty so "no devices" differs from "no driver", and returns how many devices the slice cap dropped.
func NodeResources(nodeName string, devices []Device) (resourceslice.DriverResources, int) {
	kept := devices[:min(len(devices), resourceapi.ResourceSliceMaxDevices)]
	published := make([]resourceapi.Device, len(kept))
	for i, d := range kept {
		published[i] = resourceapi.Device{Name: d.Name, Attributes: d.Attributes()}
	}
	return resourceslice.DriverResources{
		Pools: map[string]resourceslice.Pool{
			nodeName: {Slices: []resourceslice.Slice{{Devices: published}}},
		},
	}, len(devices) - len(kept)
}
