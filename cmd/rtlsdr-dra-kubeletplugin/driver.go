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

	"github.com/jacaudi/rtlsdr-dra/internal/rtlsdr"
)

// preparedDevice is one dongle mapped into one claim.
type preparedDevice struct {
	DeviceName string   // ResourceSlice device name, e.g. rtlsdr-35650102
	Serial     string   // EEPROM serial
	DevNode    string   // resolved at prepare time, not cached from publish
	Requests   []string // claim request names this device satisfies
}

type driver struct {
	client     coreclientset.Interface
	helper     *kubeletplugin.Helper
	cdi        *cdiHandler
	sysfs      fs.FS
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

	devices, duplicates, err := rtlsdr.Discover(d.sysfs)
	if err != nil {
		return fmt.Errorf("discover dongles: %w", err)
	}
	for _, serial := range duplicates {
		// Two sticks answering the same serial make a pinned claim ambiguous, and
		// rtl_tcp -d <serial> would pick arbitrarily. Loud, but not fatal.
		logger.Error(nil, "Duplicate RTL-SDR serial; reprogram one with rtl_eeprom -s",
			"serial", serial)
	}

	logger.V(2).Info("Publishing dongles", "count", len(devices), "node", d.nodeName)
	return d.helper.PublishResources(ctx, rtlsdr.NodeResources(d.nodeName, devices))
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

		// Resolve the node fresh from the serial rather than trusting a path
		// captured at publish time; the dongle may have re-enumerated since.
		dev, err := d.lookup(res.Device)
		if err != nil {
			return kubeletplugin.PrepareResult{
				Err: fmt.Errorf("claim %s: %w", claim.UID, err),
			}
		}

		devices = append(devices, preparedDevice{
			DeviceName: res.Device,
			Serial:     dev.Serial,
			DevNode:    dev.DevNode(),
			Requests:   []string{res.Request},
		})
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

// lookup maps a published device name back to the dongle currently attached.
func (d *driver) lookup(deviceName string) (rtlsdr.Device, error) {
	all, _, err := rtlsdr.Discover(d.sysfs)
	if err != nil {
		return rtlsdr.Device{}, fmt.Errorf("discover dongles: %w", err)
	}
	for _, dev := range all {
		if dev.DeviceName() == deviceName {
			return dev, nil
		}
	}
	return rtlsdr.Device{}, fmt.Errorf("device %q is no longer attached", deviceName)
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
