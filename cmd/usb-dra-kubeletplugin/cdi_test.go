package main

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdispec "tags.cncf.io/container-device-interface/specs-go"
)

const testClaimUID = "5f4ebd22-3fc4-45f9-a00c-1c5c6b6b6792"

func TestWriteClaimSpec(t *testing.T) {
	dir := t.TempDir()
	h, err := newCDIHandler(dir, "usb.jacaudi.dev")
	if err != nil {
		t.Fatalf("newCDIHandler: %v", err)
	}

	devices := []preparedDevice{{
		DeviceName: "usb-0bda-2838-35650102",
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
	if !strings.Contains(spec, testClaimUID+"-usb-0bda-2838-35650102") {
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
		{DeviceName: "usb-0bda-2838-35650102", Serial: "35650102", DevNode: "/dev/bus/usb/001/005"},
		{DeviceName: "usb-0bda-2838-37728719", Serial: "37728719", DevNode: "/dev/bus/usb/002/005"},
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

	want := h.qualifiedNames(testClaimUID, []string{"usb-0bda-2838-35650102", "usb-0bda-2838-37728719"})
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

	devices := []preparedDevice{{DeviceName: "usb-0bda-2838-35650102", Serial: "35650102", DevNode: "/dev/bus/usb/001/005"}}
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

	got := h.qualifiedNames(testClaimUID, []string{"usb-0bda-2838-35650102"})
	want := "k8s.usb.jacaudi.dev/usb=" + testClaimUID + "-usb-0bda-2838-35650102"
	if len(got) != 1 || got[0] != want {
		t.Errorf("qualifiedNames = %v, want [%s]", got, want)
	}
}

// readSpec parses the only CDI spec file in dir, as written to disk.
func readSpec(t *testing.T, dir string) cdispec.Spec {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("want exactly 1 spec file, got %v (err %v)", entries, err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var spec cdispec.Spec
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	return spec
}

func TestWriteClaimSpecWithTTYs(t *testing.T) {
	dir := t.TempDir()
	h, err := newCDIHandler(dir, "usb.jacaudi.dev")
	if err != nil {
		t.Fatalf("newCDIHandler: %v", err)
	}

	devices := []preparedDevice{{
		DeviceName: "usb-239a-8029-fd14948954cd1f4f",
		DevNode:    "/dev/bus/usb/005/002",
		Serial:     "FD14948954CD1F4F",
		TTYs:       []string{"/dev/ttyACM0"},
		Aliases:    []string{"/dev/dra/lora/tty0"},
		Requests:   []string{"lora"},
	}}
	if err := h.writeClaimSpec(testClaimUID, devices); err != nil {
		t.Fatalf("writeClaimSpec: %v", err)
	}

	spec := readSpec(t, dir)
	if len(spec.Devices) != 1 {
		t.Fatalf("got %d spec devices, want 1", len(spec.Devices))
	}

	// Host node first, then its alias; only Path, HostPath and Permissions are set.
	wantNodes := []*cdispec.DeviceNode{
		{Path: "/dev/bus/usb/005/002", HostPath: "/dev/bus/usb/005/002", Permissions: "rw"},
		{Path: "/dev/ttyACM0", HostPath: "/dev/ttyACM0", Permissions: "rw"},
		{Path: "/dev/dra/lora/tty0", HostPath: "/dev/ttyACM0", Permissions: "rw"},
	}
	edits := spec.Devices[0].ContainerEdits
	if !reflect.DeepEqual(edits.DeviceNodes, wantNodes) {
		t.Errorf("device nodes = %+v, want %+v", edits.DeviceNodes, wantNodes)
	}
	wantEnv := []string{"USB_DEVNODE=/dev/bus/usb/005/002", "USB_SERIAL=FD14948954CD1F4F", "USB_TTY=/dev/ttyACM0"}
	if !slices.Equal(edits.Env, wantEnv) {
		t.Errorf("env = %v, want %v", edits.Env, wantEnv)
	}
}

func TestWriteClaimSpecPortIdentityNoSerialEnv(t *testing.T) {
	dir := t.TempDir()
	h, err := newCDIHandler(dir, "usb.jacaudi.dev")
	if err != nil {
		t.Fatalf("newCDIHandler: %v", err)
	}

	devices := []preparedDevice{{
		DeviceName: "usb-0bda-2838-p-pnp0d10-04-v2-1",
		DevNode:    "/dev/bus/usb/005/002",
		Requests:   []string{"radio"},
	}}
	if err := h.writeClaimSpec(testClaimUID, devices); err != nil {
		t.Fatalf("writeClaimSpec: %v", err)
	}

	spec := readSpec(t, dir)
	wantEnv := []string{"USB_DEVNODE=/dev/bus/usb/005/002"}
	if got := spec.Devices[0].ContainerEdits.Env; !slices.Equal(got, wantEnv) {
		t.Errorf("env = %v, want %v (no USB_SERIAL for port identity, no USB_TTY without ttys)", got, wantEnv)
	}
}

func TestAssignAliases(t *testing.T) {
	devices := []preparedDevice{
		{DeviceName: "a", TTYs: []string{"/dev/ttyACM0"}, Requests: []string{"lora"}},
		{DeviceName: "b", TTYs: []string{"/dev/ttyUSB0", "/dev/ttyUSB1"}, Requests: []string{"lora/primary"}},
		{DeviceName: "c", Requests: []string{"radio"}},
	}
	assignAliases(devices)

	want := [][]string{
		{"/dev/dra/lora/tty0"},
		{"/dev/dra/lora/tty1", "/dev/dra/lora/tty2"},
		nil,
	}
	for i, dev := range devices {
		if !reflect.DeepEqual(dev.Aliases, want[i]) {
			t.Errorf("device %s Aliases = %#v, want %#v", dev.DeviceName, dev.Aliases, want[i])
		}
	}
}
