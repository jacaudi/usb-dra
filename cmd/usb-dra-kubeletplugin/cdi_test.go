package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
)

const testClaimUID = "5f4ebd22-3fc4-45f9-a00c-1c5c6b6b6792"

func TestWriteClaimSpec(t *testing.T) {
	dir := t.TempDir()
	h, err := newCDIHandler(dir, "usb.jacaudi.dev")
	if err != nil {
		t.Fatalf("newCDIHandler: %v", err)
	}

	devices := []preparedDevice{{
		DeviceName: "rtlsdr-35650102",
		Serial:     "35650102",
		DevNode:    "/dev/bus/usb/001/005",
		Requests:   []string{"radio"},
	}}

	if err := h.writeClaimSpec(testClaimUID, devices); err != nil {
		t.Fatalf("writeClaimSpec: %v", err)
	}

	// The CDI cache picks the extension itself (.yaml by default), so match any file.
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("want exactly 1 spec file, got %v (err %v)", entries, err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	spec := string(raw)

	// The device node is what actually reaches the container.
	if !strings.Contains(spec, "/dev/bus/usb/001/005") {
		t.Errorf("spec is missing the device node:\n%s", spec)
	}
	// Major/minor must be absent so the CDI runtime stats the host path for them.
	if strings.Contains(spec, "major:") {
		t.Errorf("spec pins major/minor; these must be resolved at injection:\n%s", spec)
	}
	// The serial env lets the container target its own dongle.
	if !strings.Contains(spec, "USB_SERIAL=35650102") {
		t.Errorf("spec is missing USB_SERIAL:\n%s", spec)
	}
	// The CDI device name must be claim-scoped so two claims never collide.
	if !strings.Contains(spec, testClaimUID+"-rtlsdr-35650102") {
		t.Errorf("CDI device name is not claim-scoped:\n%s", spec)
	}
}

func TestClaimSpecIsValidCDI(t *testing.T) {
	dir := t.TempDir()
	h, err := newCDIHandler(dir, "usb.jacaudi.dev")
	if err != nil {
		t.Fatalf("newCDIHandler: %v", err)
	}

	devices := []preparedDevice{
		{DeviceName: "rtlsdr-35650102", Serial: "35650102", DevNode: "/dev/bus/usb/001/005"},
		{DeviceName: "rtlsdr-37728719", Serial: "37728719", DevNode: "/dev/bus/usb/002/005"},
	}
	if err := h.writeClaimSpec(testClaimUID, devices); err != nil {
		t.Fatalf("writeClaimSpec: %v", err)
	}

	// Re-read through a fresh cache: this parses and validates the spec exactly as
	// the container runtime would, so an unparseable spec fails here rather than
	// at pod start.
	cache, err := cdiapi.NewCache(cdiapi.WithSpecDirs(dir))
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	if errs := cache.GetErrors(); len(errs) > 0 {
		t.Fatalf("CDI cache reported spec errors: %v", errs)
	}

	want := h.qualifiedNames(testClaimUID, []string{"rtlsdr-35650102", "rtlsdr-37728719"})
	have := map[string]bool{}
	for _, name := range cache.ListDevices() {
		have[name] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("device %q not registered; cache has %v", w, cache.ListDevices())
		}
	}
}

func TestDeleteClaimSpec(t *testing.T) {
	dir := t.TempDir()
	h, err := newCDIHandler(dir, "usb.jacaudi.dev")
	if err != nil {
		t.Fatalf("newCDIHandler: %v", err)
	}

	devices := []preparedDevice{{DeviceName: "rtlsdr-35650102", Serial: "35650102", DevNode: "/dev/bus/usb/001/005"}}
	if err := h.writeClaimSpec(testClaimUID, devices); err != nil {
		t.Fatalf("writeClaimSpec: %v", err)
	}
	if err := h.deleteClaimSpec(testClaimUID); err != nil {
		t.Fatalf("deleteClaimSpec: %v", err)
	}

	files, _ := os.ReadDir(dir)
	if len(files) != 0 {
		t.Errorf("spec file survived deletion: %v", files)
	}
}

func TestQualifiedNames(t *testing.T) {
	h, err := newCDIHandler(t.TempDir(), "usb.jacaudi.dev")
	if err != nil {
		t.Fatalf("newCDIHandler: %v", err)
	}

	got := h.qualifiedNames(testClaimUID, []string{"rtlsdr-35650102"})
	want := "k8s.usb.jacaudi.dev/usb=" + testClaimUID + "-rtlsdr-35650102"
	if len(got) != 1 || got[0] != want {
		t.Errorf("qualifiedNames = %v, want [%s]", got, want)
	}
}
