package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
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
	DevNode    string   // resolved at prepare time, not cached from publish
	Serial     string   // serial of a serial-identity device, "" for a port-identity one
	Port       string   // controller and port chain, recorded so a renamed device on the same port is still caught
	TTYs       []string // host tty paths, interface order then tty order
	Aliases    []string // Aliases[i] is the container path for TTYs[i]
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

	checkpointPath string

	mu    sync.Mutex
	state checkpoint
}

// newDriverCore builds everything except the kubelet registration: the CDI handler and the loaded checkpoint.
func newDriverCore(cfg config) (*driver, error) {
	cdi, err := newCDIHandler(cfg.cdiRoot, cfg.driverName)
	if err != nil {
		return nil, err
	}

	path := filepath.Join(cfg.pluginDir, checkpointFile)
	state, err := loadCheckpoint(path)
	if err != nil {
		return nil, err
	}

	return &driver{
		cdi:            cdi,
		sysfs:          cfg.sysfs,
		filter:         cfg.filter,
		nodeName:       cfg.nodeName,
		driverName:     cfg.driverName,
		checkpointPath: path,
		state:          state,
	}, nil
}

func newDriver(ctx context.Context, cfg config, client coreclientset.Interface) (*driver, error) {
	d, err := newDriverCore(cfg)
	if err != nil {
		return nil, err
	}
	d.client = client

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
	d.mu.Lock()
	defer d.mu.Unlock()

	uid := string(claim.UID)
	devices, err := d.resolveClaim(claim)
	if err != nil {
		return kubeletplugin.PrepareResult{Err: fmt.Errorf("claim %s: %w", uid, err)}
	}

	// The spec is rewritten even for a known claim: re-prepare picks up new nodes and rebuilds a spec a reboot wiped.
	assignAliases(devices)
	if err := d.cdi.writeClaimSpec(uid, devices); err != nil {
		return kubeletplugin.PrepareResult{Err: fmt.Errorf("claim %s: %w", uid, err)}
	}

	// A failed save keeps the old state; the spec stays and unprepare removes it.
	next := d.state.with(uid, checkpointEntry(claim, devices))
	if err := next.save(d.checkpointPath); err != nil {
		return kubeletplugin.PrepareResult{Err: fmt.Errorf("claim %s: %w", uid, err)}
	}
	d.state = next

	klog.FromContext(ctx).Info("Prepared claim", "uid", uid, "devices", len(devices))
	return kubeletplugin.PrepareResult{Devices: d.toKubeletDevices(claim, devices)}
}

// resolveClaim maps the claim's allocation for this driver to the devices attached now and refuses any another claim holds.
func (d *driver) resolveClaim(claim *resourceapi.ResourceClaim) ([]preparedDevice, error) {
	if claim.Status.Allocation == nil {
		return nil, errors.New("not allocated")
	}

	var results []resourceapi.DeviceRequestAllocationResult
	for _, res := range claim.Status.Allocation.Devices.Results {
		if res.Driver != d.driverName {
			continue
		}
		if res.AdminAccess != nil && *res.AdminAccess {
			return nil, errors.New("admin access is not supported")
		}
		results = append(results, res)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("allocated no %s devices", d.driverName)
	}

	scan, err := usb.Discover(d.sysfs, d.filter)
	if err != nil {
		return nil, fmt.Errorf("discover usb devices: %w", err)
	}
	attached := make(map[string]usb.Device, len(scan.Devices))
	ports := make(map[string]string, len(scan.Devices))
	for _, dev := range scan.Devices {
		attached[dev.Name] = dev
		ports[dev.Name] = dev.Port
	}

	devices := make([]preparedDevice, 0, len(results))
	for _, res := range results {
		dev, ok := attached[res.Device]
		if !ok {
			return nil, fmt.Errorf("device %q is no longer attached", res.Device)
		}
		prepared := preparedDevice{
			DeviceName: res.Device,
			DevNode:    dev.DevNode(),
			Port:       dev.Port,
			TTYs:       dev.TTYs(),
			Requests:   []string{res.Request},
		}
		if dev.Identity == usb.IdentitySerial {
			prepared.Serial = dev.Serial
		}
		devices = append(devices, prepared)
	}

	for _, dev := range devices {
		if other, held := d.state.holder(dev.DeviceName, dev.Port, string(claim.UID), ports); held {
			return nil, fmt.Errorf("device %s (port %s) is held by claim %s, not available to claim %s", dev.DeviceName, dev.Port, other, claim.UID)
		}
	}
	return devices, nil
}

// checkpointEntry records the claim's devices in allocation-result order.
func checkpointEntry(claim *resourceapi.ResourceClaim, devices []preparedDevice) checkpointClaim {
	entry := checkpointClaim{Namespace: claim.Namespace, Name: claim.Name}
	for _, dev := range devices {
		entry.Devices = append(entry.Devices, checkpointDevice{Device: dev.DeviceName, Request: dev.Requests[0], Port: dev.Port})
	}
	return entry
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
	defer d.mu.Unlock()

	uid := string(claim.UID)
	if err := d.cdi.deleteClaimSpec(uid); err != nil {
		return fmt.Errorf("remove CDI spec for claim %s: %w", uid, err)
	}

	// An unknown claim has no entry; unprepare is idempotent and still succeeds.
	if _, known := d.state.Claims[uid]; known {
		next := d.state.without(uid)
		if err := next.save(d.checkpointPath); err != nil {
			return fmt.Errorf("claim %s: %w", uid, err)
		}
		d.state = next
	}

	klog.FromContext(ctx).Info("Unprepared claim", "uid", uid)
	return nil
}

// HandleError is called by the helper for background errors.
func (d *driver) HandleError(ctx context.Context, err error, msg string) {
	utilruntime.HandleErrorWithContext(ctx, err, msg)
	if !errors.Is(err, kubeletplugin.ErrRecoverable) {
		klog.FromContext(ctx).Error(err, "Fatal background error", "msg", msg)
	}
}
