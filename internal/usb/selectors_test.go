package usb

import (
	"bufio"
	"context"
	"errors"
	"io"
	"maps"
	"os"
	"slices"
	"testing"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/dynamic-resource-allocation/cel"
	sigsyaml "sigs.k8s.io/yaml"
)

type claimSel struct{ Class, Expr string }

// readDocs splits the YAML file at path into its non-empty documents.
func readDocs(t *testing.T, path string) [][]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	t.Cleanup(func() { _ = f.Close() })
	r := yaml.NewYAMLReader(bufio.NewReader(f))
	var docs [][]byte
	for {
		doc, err := r.Read()
		if errors.Is(err, io.EOF) {
			return docs
		}
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if len(doc) > 0 {
			docs = append(docs, doc)
		}
	}
}

// loadSelectors returns the selector of each DeviceClass the chart renders by default and each example claim's class and first selector, "" when it has none.
func loadSelectors(t *testing.T) (classes map[string]string, claims map[string]claimSel) {
	t.Helper()
	classes = renderedDeviceClasses(t)
	claims = map[string]claimSel{}
	for _, doc := range readDocs(t, "../../deploy/claims.yaml") {
		var rct resourceapi.ResourceClaimTemplate
		if err := sigsyaml.UnmarshalStrict(doc, &rct); err != nil {
			t.Fatalf("decode ResourceClaimTemplate: %v", err)
		}
		reqs := rct.Spec.Spec.Devices.Requests
		if len(reqs) == 0 || reqs[0].Exactly == nil {
			t.Fatalf("ResourceClaimTemplate %s has no exactly request", rct.Name)
		}
		sel := claimSel{Class: reqs[0].Exactly.DeviceClassName}
		if s := reqs[0].Exactly.Selectors; len(s) > 0 && s[0].CEL != nil {
			sel.Expr = s[0].CEL.Expression
		}
		claims[rct.Name] = sel
	}
	return classes, claims
}

type selDevice struct {
	Name string
	Dev  cel.Device
}

func fromUSB(name string, d Device) selDevice {
	return selDevice{Name: name, Dev: cel.Device{Driver: DriverName, Attributes: d.Attributes()}}
}

// selectorDevices returns the fixture RAK and RTL-SDRs, a port-identity RTL-SDR whose serial is duplicated, one with no serial, and a device from another driver.
func selectorDevices(t *testing.T) []selDevice {
	t.Helper()
	scan, err := Discover(loadFixture(t, "talos-05-wrk"), defaultFilter)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	return []selDevice{
		fromUSB("rtl-35650102", findDevice(t, scan.Devices, "usb-0bda-2838-35650102")),
		fromUSB("rtl-93610062", findDevice(t, scan.Devices, "usb-0bda-2838-93610062")),
		fromUSB("rak", findDevice(t, scan.Devices, "usb-239a-8029-fd14948954cd1f4f")),
		fromUSB("dupe", Device{Identity: IdentityPort, Vendor: "0bda", Product: "2838", Serial: "35650102", Port: "PNP0D10:09-v2-1"}),
		fromUSB("noserial", Device{Identity: IdentityPort, Vendor: "0bda", Product: "2838", Port: "PNP0D10:07-v2-1"}),
		{Name: "foreign", Dev: cel.Device{
			Driver:     "gpu.example.com",
			Attributes: map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{"model": {StringValue: new("x")}},
		}},
	}
}

// evaluate compiles expr and runs it against d.
func evaluate(ctx context.Context, t *testing.T, expr string, d selDevice) (bool, error) {
	t.Helper()
	result := cel.GetCompiler(cel.Features{}).CompileCELExpression(expr, cel.Options{})
	if result.Error != nil {
		t.Fatalf("compile %q: %v", expr, result.Error)
	}
	got, _, err := result.DeviceMatches(ctx, d.Dev)
	return got, err
}

// matchingNames returns the names of the devices expr matches, failing the test on any evaluation error.
func matchingNames(t *testing.T, expr string, devices []selDevice) []string {
	t.Helper()
	var names []string
	for _, d := range devices {
		got, err := evaluate(t.Context(), t, expr, d)
		if err != nil {
			t.Errorf("%q on %s: %v", expr, d.Name, err)
		}
		if got {
			names = append(names, d.Name)
		}
	}
	return names
}

func TestSelectorsCompile(t *testing.T) {
	classes, claims := loadSelectors(t)
	if len(classes) == 0 || len(claims) == 0 {
		t.Fatalf("loaded %d classes and %d claims, want some of each", len(classes), len(claims))
	}
	var exprs []string
	exprs = slices.AppendSeq(exprs, maps.Values(classes))
	for _, c := range claims {
		if c.Expr != "" {
			exprs = append(exprs, c.Expr)
		}
	}
	for _, expr := range exprs {
		if result := cel.GetCompiler(cel.Features{}).CompileCELExpression(expr, cel.Options{}); result.Error != nil {
			t.Errorf("compile %q: %v", expr, result.Error)
		}
	}
}

func TestClassSelectorsNeverError(t *testing.T) {
	classes, _ := loadSelectors(t)
	devices := selectorDevices(t)
	for _, name := range slices.Sorted(maps.Keys(classes)) {
		t.Run(name, func(t *testing.T) {
			matchingNames(t, classes[name], devices)
		})
	}
}

func TestClassSelectorsMatch(t *testing.T) {
	classes, _ := loadSelectors(t)
	devices := selectorDevices(t)
	tests := []struct {
		class string
		want  []string
	}{
		{"rtl-sdr.usb.jacaudi.dev", []string{"rtl-35650102", "rtl-93610062", "dupe", "noserial"}},
		{"rak4631.usb.jacaudi.dev", []string{"rak"}},
	}
	for _, tt := range tests {
		t.Run(tt.class, func(t *testing.T) {
			expr, ok := classes[tt.class]
			if !ok {
				t.Fatalf("DeviceClass %s not shipped; have %v", tt.class, slices.Sorted(maps.Keys(classes)))
			}
			if got := matchingNames(t, expr, devices); !slices.Equal(got, tt.want) {
				t.Errorf("matches %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClaimSelectorsMatch(t *testing.T) {
	classes, claims := loadSelectors(t)
	devices := selectorDevices(t)
	tests := []struct {
		claim, class string
		noSelector   bool
		want         []string
	}{
		{claim: "rtl-sdr-any", class: "rtl-sdr.usb.jacaudi.dev", noSelector: true},
		{claim: "rtl-sdr-35650102", class: "rtl-sdr.usb.jacaudi.dev", want: []string{"rtl-35650102"}},
		{claim: "rak4631-lora-port", class: "rak4631.usb.jacaudi.dev", want: []string{"rak"}},
	}
	for _, tt := range tests {
		t.Run(tt.claim, func(t *testing.T) {
			c, ok := claims[tt.claim]
			if !ok {
				t.Fatalf("claim %s not shipped; have %v", tt.claim, slices.Sorted(maps.Keys(claims)))
			}
			if c.Class != tt.class {
				t.Fatalf("class = %q, want %q", c.Class, tt.class)
			}
			if tt.noSelector {
				if c.Expr != "" {
					t.Errorf("Expr = %q, want none", c.Expr)
				}
				return
			}
			classExpr, ok := classes[c.Class]
			if !ok {
				t.Fatalf("DeviceClass %s not shipped", c.Class)
			}
			var admitted []selDevice
			for _, d := range devices {
				got, err := evaluate(t.Context(), t, classExpr, d)
				if err != nil {
					t.Fatalf("class %s on %s: %v", c.Class, d.Name, err)
				}
				if got {
					admitted = append(admitted, d)
				}
			}
			if got := matchingNames(t, c.Expr, admitted); !slices.Equal(got, tt.want) {
				t.Errorf("matches %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSelectorsSerialPinSkipsDuplicate(t *testing.T) {
	_, claims := loadSelectors(t)
	expr := claims["rtl-sdr-35650102"].Expr
	for _, d := range selectorDevices(t) {
		if d.Name != "dupe" && d.Name != "noserial" {
			continue
		}
		t.Run(d.Name, func(t *testing.T) {
			got, err := evaluate(t.Context(), t, expr, d)
			if err != nil {
				t.Errorf("error = %v, want none", err)
			}
			if got {
				t.Errorf("serial pin matched %s, want no match", d.Name)
			}
		})
	}
}
