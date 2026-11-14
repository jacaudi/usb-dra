package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sync"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	coreclientset "k8s.io/client-go/kubernetes"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/klog/v2"

	"github.com/jacaudi/usb-dra/internal/usb"
)

// preparedDevice is one USB device mapped into one claim.
type preparedDevice struct {
	DeviceName string   // ResourceSlice device name, e.g. usb-0bda-2838-35650102
	Serial     string   // serial of a serial-identity device, "" for a port-identity one
	DevNode    string   // resolved at prepare time, not cached from publish
	Requests   []string // claim request names this device satisfies
}

type driver struct {
	client     coreclientset.Interface
	helper     *kubeletplugin.Helper
	cdi        *cdiHandler
	sysfs      fs.FS
	filter     usb.Filter
	nodeName   string
	driverName string

	mu       sync.Mutex
	prepared map[types.UID][]preparedDevice
}

func newDriver(ctx context.Context, cfg config, client coreclientset.Interface) (*driver, error) {
	cdi, err := newCDIHandler(cfg.cdiRoot, cfg.driverName)
	if err != nil {
		return nil, err
	}

	d := &driver{
		client:     client,
		cdi:        cdi,
		sysfs:      cfg.sysfs,
		filter:     cfg.filter,
		nodeName:   cfg.nodeName,
		driverName: cfg.driverName,
		prepared:   make(map[types.UID][]preparedDevice),
	}

	helper, err := kubeletplugin.Start(ctx, d,
		kubeletplugin.KubeClient(client),
		kubeletplugin.NodeName(cfg.nodeName),
		kubeletplugin.DriverName(cfg.driverName),
		kubeletplugin.RegistrarDirectoryPath(cfg.registrarDir),
		kubeletplugin.PluginDataDirectoryPath(cfg.pluginDir),
	)
	if err != nil {
		return nil, fmt.Errorf("start kubelet plugin: %w", err)
	}
	d.helper = helper

	return d, nil
}

// publish re-scans sysfs and republishes this node's pool. Safe to call repeatedly;
// the resourceslice controller diffs against what is already published.
func (d *driver) publish(ctx context.Context) error {
	logger := klog.FromContext(ctx)

	scan, err := usb.Discover(d.sysfs, d.filter)
	if err != nil {
		return fmt.Errorf("discover usb devices: %w", err)
	}
	for _, serial := range scan.DuplicateSerials {
		// Devices sharing a serial fall back to port identity, so a claim pinned by that serial matches none of them.
		logger.Error(nil, "Duplicate USB serial; the devices are named by port until the serials differ", "serial", serial)
	}
	for _, skip := range scan.Skipped {
		if skip.Severe {
			logger.Error(nil, "Skipping USB device", "dir", skip.Dir, "reason", skip.Reason)
		} else {
			logger.V(2).Info("Skipping USB device", "dir", skip.Dir, "reason", skip.Reason)
		}
	}

	resources, dropped := usb.NodeResources(d.nodeName, scan.Devices)
	if dropped > 0 {
		logger.Error(nil, "Too many USB devices for one ResourceSlice; the rest are not published", "dropped", dropped)
	}

	logger.V(2).Info("Publishing USB devices", "count", len(scan.Devices)-dropped, "node", d.nodeName)
	return d.helper.PublishResources(ctx, resources)
}

func (d *driver) Shutdown() {
	if d.helper != nil {
		d.helper.Stop()
	}
}

func (d *driver) PrepareResourceClaims(ctx context.Context, claims []*resourceapi.ResourceClaim) (map[types.UID]kubeletplugin.PrepareResult, error) {
	result := make(map[types.UID]kubeletplugin.PrepareResult, len(claims))
	for _, claim := range claims {
		result[claim.UID] = d.prepareOne(ctx, claim)
	}
	return result, nil
}

func (d *driver) prepareOne(ctx context.Context, claim *resourceapi.ResourceClaim) kubeletplugin.PrepareResult {
	logger := klog.FromContext(ctx)

	d.mu.Lock()
	existing, ok := d.prepared[claim.UID]
	d.mu.Unlock()
	if ok {
		// Prepare is idempotent: the kubelet may retry after a restart.
		return kubeletplugin.PrepareResult{Devices: d.toKubeletDevices(claim, existing)}
	}

	if claim.Status.Allocation == nil {
		return kubeletplugin.PrepareResult{Err: fmt.Errorf("claim %s is not allocated", claim.UID)}
	}

	var devices []preparedDevice
	for _, res := range claim.Status.Allocation.Devices.Results {
		if res.Driver != d.driverName {
			continue
		}

		// Resolve the node fresh by name rather than trusting a path captured at publish time; the device may have re-enumerated since.
		dev, err := usb.FindByName(d.sysfs, d.filter, res.Device)
		if err != nil {
			return kubeletplugin.PrepareResult{
				Err: fmt.Errorf("claim %s: %w", claim.UID, err),
			}
		}

		prepared := preparedDevice{
			DeviceName: res.Device,
			DevNode:    dev.DevNode(),
			Requests:   []string{res.Request},
		}
		if dev.Identity == usb.IdentitySerial {
			prepared.Serial = dev.Serial
		}
		devices = append(devices, prepared)
	}

	if len(devices) == 0 {
		return kubeletplugin.PrepareResult{
			Err: fmt.Errorf("claim %s allocated no %s devices", claim.UID, d.driverName),
		}
	}

	if err := d.cdi.writeClaimSpec(string(claim.UID), devices); err != nil {
		return kubeletplugin.PrepareResult{Err: fmt.Errorf("claim %s: %w", claim.UID, err)}
	}

	d.mu.Lock()
	d.prepared[claim.UID] = devices
	d.mu.Unlock()

	logger.Info("Prepared claim", "uid", claim.UID, "devices", len(devices))
	return kubeletplugin.PrepareResult{Devices: d.toKubeletDevices(claim, devices)}
}

func (d *driver) toKubeletDevices(claim *resourceapi.ResourceClaim, devices []preparedDevice) []kubeletplugin.Device {
	out := make([]kubeletplugin.Device, 0, len(devices))
	for _, dev := range devices {
		out = append(out, kubeletplugin.Device{
			Requests:     dev.Requests,
			PoolName:     d.nodeName,
			DeviceName:   dev.DeviceName,
			CDIDeviceIDs: d.cdi.qualifiedNames(string(claim.UID), []string{dev.DeviceName}),
		})
	}
	return out
}

func (d *driver) UnprepareResourceClaims(ctx context.Context, claims []kubeletplugin.NamespacedObject) (map[types.UID]error, error) {
	result := make(map[types.UID]error, len(claims))
	for _, claim := range claims {
		result[claim.UID] = d.unprepareOne(ctx, claim)
	}
	return result, nil
}

func (d *driver) unprepareOne(ctx context.Context, claim kubeletplugin.NamespacedObject) error {
	d.mu.Lock()
	_, ok := d.prepared[claim.UID]
	d.mu.Unlock()
	if !ok {
		// Already released, or never prepared here. Unprepare must be idempotent.
		return nil
	}

	if err := d.cdi.deleteClaimSpec(string(claim.UID)); err != nil {
		return fmt.Errorf("remove CDI spec for claim %s: %w", claim.UID, err)
	}

	d.mu.Lock()
	delete(d.prepared, claim.UID)
	d.mu.Unlock()

	klog.FromContext(ctx).Info("Unprepared claim", "uid", claim.UID)
	return nil
}

// HandleError is called by the helper for background errors.
func (d *driver) HandleError(ctx context.Context, err error, msg string) {
	utilruntime.HandleErrorWithContext(ctx, err, msg)
	if !errors.Is(err, kubeletplugin.ErrRecoverable) {
		klog.FromContext(ctx).Error(err, "Fatal background error", "msg", msg)
	}
}
