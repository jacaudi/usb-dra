// Command rtlsdr-dra-kubeletplugin publishes locally-attached RTL-SDR dongles as
// DRA devices and maps them into containers via CDI.
//
// Each dongle becomes one device in this node's pool, carrying its EEPROM serial as
// a selectable attribute. A claim with no selector draws any free radio; a claim
// with a CEL selector on the serial pins one specific stick.
package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"github.com/jacaudi/rtlsdr-dra/internal/rtlsdr"
)

type config struct {
	driverName   string
	nodeName     string
	cdiRoot      string
	registrarDir string
	pluginDir    string
	sysfs        fs.FS
	rescan       time.Duration
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		driverName   = flag.String("driver-name", rtlsdr.DriverName, "DRA driver name")
		nodeName     = flag.String("node-name", os.Getenv("NODE_NAME"), "name of the node this instance runs on")
		cdiRoot      = flag.String("cdi-root", "/var/run/cdi", "directory for generated CDI specs")
		registrarDir = flag.String("kubelet-registrar-dir", "/var/lib/kubelet/plugins_registry", "kubelet plugin registrar directory")
		pluginDir    = flag.String("kubelet-plugin-dir", "", "kubelet plugin data directory (default /var/lib/kubelet/plugins/<driver-name>)")
		sysfsRoot    = flag.String("sysfs-root", "/", "filesystem root to read sysfs from")
		rescan       = flag.Duration("rescan-interval", 30*time.Second, "how often to re-scan for hotplugged dongles")
	)
	klog.InitFlags(nil)
	flag.Parse()

	if *nodeName == "" {
		return fmt.Errorf("--node-name is required (or set NODE_NAME); it names this node's device pool")
	}
	if *pluginDir == "" {
		*pluginDir = filepath.Join("/var/lib/kubelet/plugins", *driverName)
	}

	cfg := config{
		driverName:   *driverName,
		nodeName:     *nodeName,
		cdiRoot:      *cdiRoot,
		registrarDir: *registrarDir,
		pluginDir:    *pluginDir,
		sysfs:        os.DirFS(*sysfsRoot),
		rescan:       *rescan,
	}

	// Fail loudly rather than silently publishing an empty pool.
	for _, dir := range []string{cfg.cdiRoot, cfg.pluginDir} {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}

	restCfg, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("build in-cluster config: %w", err)
	}
	client, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return fmt.Errorf("build kubernetes client: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	logger := klog.FromContext(ctx)

	d, err := newDriver(ctx, cfg, client)
	if err != nil {
		return err
	}
	defer d.Shutdown()

	if err := d.publish(ctx); err != nil {
		return fmt.Errorf("initial publish: %w", err)
	}
	logger.Info("Driver started", "driver", cfg.driverName, "node", cfg.nodeName)

	// Re-scan on a timer so a hotplugged dongle appears without a restart. sysfs
	// offers no portable watch for USB add/remove that works from a container.
	ticker := time.NewTicker(cfg.rescan)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("Shutting down")
			return nil
		case <-ticker.C:
			if err := d.publish(ctx); err != nil {
				// Recoverable: keep serving the devices already published.
				logger.Error(err, "Re-publish failed; retrying next tick")
			}
		}
	}
}
