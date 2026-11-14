// Command usb-dra-kubeletplugin publishes local USB devices as DRA devices and maps them into containers via CDI.
package main

import (
	"context"
	"errors"
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

	"github.com/jacaudi/usb-dra/internal/usb"
)

type config struct {
	driverName   string
	nodeName     string
	cdiRoot      string
	registrarDir string
	pluginDir    string
	sysfs        fs.FS
	filter       usb.Filter
	rescan       time.Duration
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// parseFlags reads and validates the command line; klog's flags are registered on the same set.
func parseFlags(args []string) (config, error) {
	flags := flag.NewFlagSet("usb-dra-kubeletplugin", flag.ContinueOnError)
	var (
		driverName     = flags.String("driver-name", usb.DriverName, "DRA driver name")
		nodeName       = flags.String("node-name", os.Getenv("NODE_NAME"), "name of the node this instance runs on")
		cdiRoot        = flags.String("cdi-root", "/var/run/cdi", "directory for generated CDI specs")
		registrarDir   = flags.String("kubelet-registrar-dir", "/var/lib/kubelet/plugins_registry", "kubelet plugin registrar directory")
		pluginDir      = flags.String("kubelet-plugin-dir", "", "kubelet plugin data directory (default /var/lib/kubelet/plugins/<driver-name>)")
		sysfsRoot      = flags.String("sysfs-root", "/", "filesystem root to read sysfs from")
		rescan         = flags.Duration("rescan-interval", 30*time.Second, "how often to re-scan for hotplugged devices")
		excludeClasses = flags.String("exclude-classes", usb.DefaultExcludeClasses, "comma-separated interface classes (two lowercase hex digits) whose devices are not published")
		includes       []string
	)
	flags.Func("include", "vid:pid or vid:pid:serial of a device to publish despite --exclude-classes; repeatable", func(s string) error {
		includes = append(includes, s)
		return nil
	})
	klog.InitFlags(flags)
	if err := flags.Parse(args); err != nil {
		return config{}, err
	}
	if flags.NArg() > 0 {
		return config{}, fmt.Errorf("unexpected arguments: %q", flags.Args())
	}
	if *rescan <= 0 {
		return config{}, errors.New("--rescan-interval must be greater than zero")
	}

	filter, err := parseFilter(*excludeClasses, includes)
	if err != nil {
		return config{}, err
	}
	if *nodeName == "" {
		return config{}, errors.New("--node-name is required (or set NODE_NAME); it names this node's device pool")
	}
	if *pluginDir == "" {
		*pluginDir = filepath.Join("/var/lib/kubelet/plugins", *driverName)
	}

	return config{
		driverName:   *driverName,
		nodeName:     *nodeName,
		cdiRoot:      *cdiRoot,
		registrarDir: *registrarDir,
		pluginDir:    *pluginDir,
		sysfs:        os.DirFS(*sysfsRoot),
		filter:       filter,
		rescan:       *rescan,
	}, nil
}

func parseFilter(excludeClasses string, includes []string) (usb.Filter, error) {
	exclude, err := usb.ParseExcludeClasses(excludeClasses)
	if err != nil {
		return usb.Filter{}, fmt.Errorf("--exclude-classes: %w", err)
	}
	filter := usb.Filter{ExcludeClasses: exclude}
	for _, s := range includes {
		inc, err := usb.ParseInclude(s)
		if err != nil {
			return usb.Filter{}, fmt.Errorf("--include: %w", err)
		}
		filter.Includes = append(filter.Includes, inc)
	}
	return filter, nil
}

func run() error {
	cfg, err := parseFlags(os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
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
