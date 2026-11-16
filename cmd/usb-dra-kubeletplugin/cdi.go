package main

import (
	"fmt"

	"k8s.io/dynamic-resource-allocation/resourceclaim"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdiparser "tags.cncf.io/container-device-interface/pkg/parser"
	cdispec "tags.cncf.io/container-device-interface/specs-go"
)

// cdiClass groups every device this driver injects.
const cdiClass = "usb"

// cdiHandler writes the CDI specs that map a USB device's nodes into the container; the kubelet hands the qualified names to the runtime.
type cdiHandler struct {
	cache  *cdiapi.Cache
	vendor string
}

func newCDIHandler(specDir, driverName string) (*cdiHandler, error) {
	cache, err := cdiapi.NewCache(cdiapi.WithSpecDirs(specDir))
	if err != nil {
		return nil, fmt.Errorf("create CDI cache in %s: %w", specDir, err)
	}
	return &cdiHandler{cache: cache, vendor: "k8s." + driverName}, nil
}

// writeClaimSpec emits one transient CDI spec for every device allocated to a claim; major and minor stay unset so the runtime reads them from the host path.
func (c *cdiHandler) writeClaimSpec(claimUID string, devices []preparedDevice) error {
	spec := &cdispec.Spec{
		Kind:    c.kind(),
		Devices: make([]cdispec.Device, 0, len(devices)),
	}

	for _, d := range devices {
		spec.Devices = append(spec.Devices, cdispec.Device{
			Name:           cdiDeviceName(claimUID, d.DeviceName),
			ContainerEdits: containerEdits(d),
		})
	}

	version, err := cdispec.MinimumRequiredVersion(spec)
	if err != nil {
		return fmt.Errorf("determine CDI spec version: %w", err)
	}
	spec.Version = version

	return c.cache.WriteSpec(spec, c.specName(claimUID))
}

// containerEdits injects the USB node, then each tty at its host path and at its alias; major/minor stay unset.
func containerEdits(d preparedDevice) cdispec.ContainerEdits {
	node := func(path, hostPath string) *cdispec.DeviceNode {
		return &cdispec.DeviceNode{Path: path, HostPath: hostPath, Permissions: "rw"}
	}

	nodes := make([]*cdispec.DeviceNode, 0, 1+2*len(d.TTYs))
	nodes = append(nodes, node(d.DevNode, d.DevNode))
	for i, tty := range d.TTYs {
		nodes = append(nodes, node(tty, tty), node(d.Aliases[i], tty))
	}

	env := []string{"USB_DEVNODE=" + d.DevNode}
	if d.Serial != "" {
		env = append(env, "USB_SERIAL="+d.Serial)
	}
	if len(d.TTYs) > 0 {
		env = append(env, "USB_TTY="+d.TTYs[0])
	}
	return cdispec.ContainerEdits{DeviceNodes: nodes, Env: env}
}

// assignAliases gives every tty the alias /dev/dra/<request>/tty<k>, counting from 0 across the devices of one request.
func assignAliases(devices []preparedDevice) {
	next := map[string]int{}
	for i := range devices {
		request := resourceclaim.BaseRequestRef(devices[i].Requests[0])
		for range devices[i].TTYs {
			devices[i].Aliases = append(devices[i].Aliases, fmt.Sprintf("/dev/dra/%s/tty%d", request, next[request]))
			next[request]++
		}
	}
}

func (c *cdiHandler) deleteClaimSpec(claimUID string) error {
	return c.cache.RemoveSpec(c.specName(claimUID))
}

// qualifiedNames returns the CDI device IDs handed back to the kubelet.
func (c *cdiHandler) qualifiedNames(claimUID string, deviceNames []string) []string {
	out := make([]string, 0, len(deviceNames))
	for _, name := range deviceNames {
		out = append(out, cdiparser.QualifiedName(c.vendor, cdiClass, cdiDeviceName(claimUID, name)))
	}
	return out
}

func (c *cdiHandler) kind() string { return c.vendor + "/" + cdiClass }

func (c *cdiHandler) specName(claimUID string) string {
	return cdiapi.GenerateTransientSpecName(c.vendor, cdiClass, claimUID)
}

// cdiDeviceName scopes a device to its claim so two claims never collide in the shared CDI namespace.
func cdiDeviceName(claimUID, deviceName string) string {
	return claimUID + "-" + deviceName
}
