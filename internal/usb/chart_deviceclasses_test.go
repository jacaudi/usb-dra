package usb

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// valuesFile writes yamlText verbatim to a temp file, so YAML coercion (1e03, 0403) happens as it would for a user's -f file, and returns the -f args for it. The directory is not t.TempDir(): that embeds the subtest name, and helm splits -f on commas.
func valuesFile(t *testing.T, yamlText string) []string {
	t.Helper()
	dir, err := os.MkdirTemp("", "usb-dra-values-")
	if err != nil {
		t.Fatalf("create values dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	p := filepath.Join(dir, "values.yaml")
	if err := os.WriteFile(p, []byte(yamlText), 0o600); err != nil {
		t.Fatalf("write values file: %v", err)
	}
	return []string{"-f", p}
}

// renderedDeviceClasses renders the chart and returns each DeviceClass's name and its only (CEL) selector expression.
func renderedDeviceClasses(t *testing.T, extra ...string) map[string]string {
	t.Helper()
	classes := map[string]string{}
	for _, d := range findKind(t, mustRender(t, "usb-dra", "kube-system", extra...), "DeviceClass") {
		var c resourceapi.DeviceClass
		if err := runtime.DefaultUnstructuredConverter.FromUnstructuredWithValidation(d.Object, &c, true); err != nil {
			t.Fatalf("decode DeviceClass %s: %v", d.GetName(), err)
		}
		if len(c.Spec.Selectors) != 1 || c.Spec.Selectors[0].CEL == nil {
			t.Fatalf("DeviceClass %s must have exactly one CEL selector, got %+v", c.Name, c.Spec.Selectors)
		}
		classes[c.Name] = c.Spec.Selectors[0].CEL.Expression
	}
	return classes
}

func TestChartDeviceClasses(t *testing.T) {
	const (
		rtl = `device.driver == "usb.jacaudi.dev" && device.attributes["usb.jacaudi.dev"].vendor == "0bda" && device.attributes["usb.jacaudi.dev"].product in ["2838", "2832"]`
		rak = `device.driver == "usb.jacaudi.dev" && device.attributes["usb.jacaudi.dev"].vendor == "239a" && device.attributes["usb.jacaudi.dev"].product in ["8029"]`
		gps = `device.driver == "usb.jacaudi.dev" && device.attributes["usb.jacaudi.dev"].vendor == "1546" && device.attributes["usb.jacaudi.dev"].product in ["01a8"]`
	)
	tests := []struct {
		name   string
		values string // layered over examples/values.yaml; "" renders the example alone
		want   map[string]string
	}{
		{
			name: "example values",
			want: map[string]string{"rtl-sdr.usb.jacaudi.dev": rtl, "rak4631.usb.jacaudi.dev": rak},
		},
		{
			name:   "null removes an example class, a new key adds a class",
			values: "deviceClasses: {rak4631: null, gps: {vendor: \"1546\", products: [\"01a8\"]}}\n",
			want:   map[string]string{"rtl-sdr.usb.jacaudi.dev": rtl, "gps.usb.jacaudi.dev": gps},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extra := []string{"-f", exampleValues}
			if tt.values != "" {
				extra = append(extra, valuesFile(t, tt.values)...)
			}
			got := renderedDeviceClasses(t, extra...)
			if !maps.Equal(got, tt.want) {
				t.Errorf("DeviceClasses = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestExampleValuesRender guards the shipped example deployment: it passes the schema and defines exactly the rtl-sdr and rak4631 classes the README and examples/claims.yaml refer to.
func TestExampleValuesRender(t *testing.T) {
	got := slices.Sorted(maps.Keys(renderedDeviceClasses(t, "-f", exampleValues)))
	want := []string{"rak4631.usb.jacaudi.dev", "rtl-sdr.usb.jacaudi.dev"}
	if !slices.Equal(got, want) {
		t.Errorf("examples/values.yaml DeviceClasses = %v, want %v", got, want)
	}
}

// TestChartDeviceClassSchemaRejects guards against a silently wrong selector: YAML turns an unquoted 1e03, 10e2 or 0403 into a number, and the schema must refuse it, along with any unknown key or malformed name. Each case names the schema path that must be reported, so a rejection for an unrelated reason cannot pass.
func TestChartDeviceClassSchemaRejects(t *testing.T) {
	tests := []struct{ name, values, wantErr string }{
		{"vendor coerced to a float", "deviceClasses: {gps: {vendor: 1e03, products: [\"01a8\"]}}\n", "'/deviceClasses/gps/vendor': got number, want string"},
		{"product coerced to a float", "deviceClasses: {gps: {vendor: \"1546\", products: [10e2]}}\n", "'/deviceClasses/gps/products/0': got number, want string"},
		{"vendor coerced to an octal int", "deviceClasses: {gps: {vendor: 0403, products: [\"01a8\"]}}\n", "'/deviceClasses/gps/vendor': got number, want string"},
		{"vendor uppercase", "deviceClasses: {gps: {vendor: \"0BDA\", products: [\"01a8\"]}}\n", "'/deviceClasses/gps/vendor': '0BDA' does not match pattern"},
		{"vendor too short", "deviceClasses: {gps: {vendor: \"bda\", products: [\"01a8\"]}}\n", "'/deviceClasses/gps/vendor': 'bda' does not match pattern"},
		{"product key instead of products", "deviceClasses: {gps: {vendor: \"1546\", products: [\"01a8\"], product: [\"2838\"]}}\n", "'/deviceClasses/gps': additional properties 'product' not allowed"},
		{"products missing", "deviceClasses: {gps: {vendor: \"1546\"}}\n", "'/deviceClasses/gps': missing property 'products'"},
		{"vendor missing", "deviceClasses: {gps: {products: [\"01a8\"]}}\n", "'/deviceClasses/gps': missing property 'vendor'"},
		{"products empty", "deviceClasses: {gps: {vendor: \"1546\", products: []}}\n", "'/deviceClasses/gps/products': minItems"},
		{"class name with underscore and capital", "deviceClasses: {Bad_Name: {vendor: \"1546\", products: [\"01a8\"]}}\n", "invalid propertyName 'Bad_Name'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := renderChart(t, "usb-dra", "kube-system", valuesFile(t, tt.values)...)
			if err == nil {
				t.Fatalf("values %q rendered, want a schema error", tt.values)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}
