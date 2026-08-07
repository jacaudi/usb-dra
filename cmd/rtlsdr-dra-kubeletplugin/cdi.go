package main

import (
	"fmt"

	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdiparser "tags.cncf.io/container-device-interface/pkg/parser"
	cdispec "tags.cncf.io/container-device-interface/specs-go"
)

// cdiClass groups every device this driver injects.
const cdiClass = "rtlsdr"

// cdiHandler writes the CDI specs that map a dongle's character device into the
// container. The kubelet passes the qualified CDI names to the runtime, which
// applies these edits when the container is created.
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

// writeClaimSpec emits one transient CDI spec describing every dongle allocated to
// a claim. Major/minor numbers are intentionally omitted: the CDI runtime fills them
// in by stat-ing the host path, which keeps this correct across re-enumeration.
func (c *cdiHandler) writeClaimSpec(claimUID string, devices []preparedDevice) error {
	spec := &cdispec.Spec{
		Kind:    c.kind(),
		Devices: make([]cdispec.Device, 0, len(devices)),
	}

	for _, d := range devices {
		spec.Devices = append(spec.Devices, cdispec.Device{
			Name: cdiDeviceName(claimUID, d.DeviceName),
			ContainerEdits: cdispec.ContainerEdits{
				DeviceNodes: []*cdispec.DeviceNode{{
					Path:        d.DevNode,
					HostPath:    d.DevNode,
					Permissions: "rw",
				}},
				Env: []string{
					// Lets the container target its own dongle, e.g. rtl_tcp -d "$RTLSDR_SERIAL".
					fmt.Sprintf("RTLSDR_SERIAL=%s", d.Serial),
					fmt.Sprintf("RTLSDR_DEVNODE=%s", d.DevNode),
				},
			},
		})
	}

	version, err := cdiapi.MinimumRequiredVersion(spec)
	if err != nil {
		return fmt.Errorf("determine CDI spec version: %w", err)
	}
	spec.Version = version

	return c.cache.WriteSpec(spec, c.specName(claimUID))
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

// cdiDeviceName scopes a device to its claim so two claims holding different radios
// never collide in the shared CDI namespace.
func cdiDeviceName(claimUID, deviceName string) string {
	return claimUID + "-" + deviceName
}
