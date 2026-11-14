package usb

import (
	"maps"
	"strings"
	"testing"
)

func TestParseExcludeClasses(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		want  map[string]bool
		token string // substring the error must contain; empty means no error expected
	}{
		{name: "default", in: DefaultExcludeClasses, want: map[string]bool{"03": true, "08": true, "e0": true}},
		{name: "empty disables the filter", in: "", want: map[string]bool{}},
		{name: "one digit", in: "3", token: `"3"`},
		{name: "non-hex digit", in: "0g", token: `"0g"`},
		{name: "empty token", in: "03,,08", token: `""`},
		{name: "uppercase", in: "E0", token: `"E0"`},
		{name: "three digits", in: "003", token: `"003"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseExcludeClasses(tt.in)
			if tt.token != "" {
				if err == nil {
					t.Fatalf("ParseExcludeClasses(%q) error = nil, want one mentioning %s", tt.in, tt.token)
				}
				if !strings.Contains(err.Error(), tt.token) {
					t.Errorf("ParseExcludeClasses(%q) error = %q, want it to mention %s", tt.in, err, tt.token)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseExcludeClasses(%q) error = %v, want nil", tt.in, err)
			}
			if !maps.Equal(got, tt.want) {
				t.Errorf("ParseExcludeClasses(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseInclude(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    Include
		wantErr bool
	}{
		{name: "vid pid", in: "239a:8029", want: Include{"239a", "8029", ""}},
		{name: "vid pid serial", in: "0bda:2838:35650102", want: Include{"0bda", "2838", "35650102"}},
		{name: "serial keeps colons and case", in: "0403:6001:A:B:C", want: Include{"0403", "6001", "A:B:C"}},
		{name: "vid only", in: "239a", wantErr: true},
		{name: "empty pid", in: "239a:", wantErr: true},
		{name: "uppercase vid", in: "239A:8029", wantErr: true},
		{name: "short pid", in: "239a:802", wantErr: true},
		{name: "empty serial", in: "239a:8029:", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseInclude(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseInclude(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParseInclude(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestFilterPublish(t *testing.T) {
	f := Filter{
		ExcludeClasses: map[string]bool{"03": true, "08": true, "e0": true},
		Includes:       []Include{{"046d", "c52b", ""}, {"1234", "5678", "abc"}},
	}
	tests := []struct {
		name    string
		filter  Filter
		vendor  string
		product string
		serial  string
		classes []string
		want    bool
	}{
		{name: "all interfaces excluded", filter: f, vendor: "0e8d", product: "c616", serial: "000000000", classes: []string{"e0", "e0", "e0"}, want: false},
		{name: "no excluded class", filter: f, vendor: "239a", product: "8029", serial: "X", classes: []string{"02", "0a"}, want: true},
		{name: "one interface not excluded", filter: f, vendor: "1a2b", product: "3c4d", classes: []string{"03", "ff"}, want: true},
		{name: "include without serial matches any serial", filter: f, vendor: "046d", product: "c52b", classes: []string{"03"}, want: true},
		{name: "include serial compares case-insensitively", filter: f, vendor: "1234", product: "5678", serial: "ABC", classes: []string{"03"}, want: true},
		{name: "include serial mismatch stays excluded", filter: f, vendor: "1234", product: "5678", serial: "other", classes: []string{"03"}, want: false},
		{name: "include serial absent stays excluded", filter: f, vendor: "1234", product: "5678", classes: []string{"03"}, want: false},
		{name: "no interfaces is published", filter: f, vendor: "0e8d", product: "c616", want: true},
		{name: "zero filter publishes everything", vendor: "0e8d", product: "c616", classes: []string{"e0"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.filter.Publish(tt.vendor, tt.product, tt.serial, tt.classes); got != tt.want {
				t.Errorf("Publish(%q, %q, %q, %v) = %v, want %v", tt.vendor, tt.product, tt.serial, tt.classes, got, tt.want)
			}
		})
	}
}
