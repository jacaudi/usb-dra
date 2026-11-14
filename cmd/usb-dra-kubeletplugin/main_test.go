package main

import (
	"strings"
	"testing"
)

func TestParseFlagsAcceptsDaemonSetArgs(t *testing.T) {
	t.Setenv("NODE_NAME", "talos-05-wrk")

	cfg, err := parseFlags([]string{"--driver-name=usb.jacaudi.dev", "--rescan-interval=30s", "-v=2", "--exclude-classes=03,08,e0"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if cfg.nodeName != "talos-05-wrk" {
		t.Errorf("nodeName = %q, want the NODE_NAME default", cfg.nodeName)
	}
	if want := "/var/lib/kubelet/plugins/usb.jacaudi.dev"; cfg.pluginDir != want {
		t.Errorf("pluginDir = %q, want %q", cfg.pluginDir, want)
	}
	if !cfg.filter.ExcludeClasses["e0"] {
		t.Errorf("ExcludeClasses = %v, want 03, 08 and e0", cfg.filter.ExcludeClasses)
	}
}

func TestParseFlagsRepeatableInclude(t *testing.T) {
	t.Setenv("NODE_NAME", "n")

	cfg, err := parseFlags([]string{"--include=239a:8029", "--include=0bda:2838:35650102"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if got := len(cfg.filter.Includes); got != 2 {
		t.Fatalf("got %d includes, want 2", got)
	}
	if got := cfg.filter.Includes[1].Serial; got != "35650102" {
		t.Errorf("second include serial = %q, want 35650102", got)
	}
}

func TestParseFlagsRejectsBadValues(t *testing.T) {
	t.Setenv("NODE_NAME", "n")

	for _, tc := range []struct{ arg, want string }{
		{"--exclude-classes=zz", "exclude-classes"},
		{"--include=239a", "include"},
		{"stray", "unexpected arguments"},
		{"--rescan-interval=0s", "rescan-interval"},
		{"--rescan-interval=-1s", "rescan-interval"},
	} {
		_, err := parseFlags([]string{tc.arg})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("parseFlags(%q) error = %v, want it to mention %q", tc.arg, err, tc.want)
		}
	}
}

func TestParseFlagsRequiresNodeName(t *testing.T) {
	t.Setenv("NODE_NAME", "")

	if _, err := parseFlags(nil); err == nil || !strings.Contains(err.Error(), "node-name") {
		t.Errorf("parseFlags error = %v, want it to require --node-name", err)
	}
}
