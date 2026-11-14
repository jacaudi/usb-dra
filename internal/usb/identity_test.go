package usb

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"testing/fstest"
)

func TestDeviceNameSanitizes(t *testing.T) {
	tests := []struct {
		name                string
		vendor, product, id string
		want                string
	}{
		{"punctuation and case", "239a", "8029", "A_b.C--d", "usb-239a-8029-a-b-c-d"},
		{"port identity", "1a86", "7523", "p-0000:00:14.0-v2-1.4", "usb-1a86-7523-p-0000-00-14-0-v2-1-4"},
		{"edges are trimmed", "239a", "8029", "__x__", "usb-239a-8029-x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deviceName(tt.vendor, tt.product, tt.id); got != tt.want {
				t.Errorf("deviceName = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLongNameIsTruncatedWithHash(t *testing.T) {
	const serial = "abcdefghij0123456789abcdefghij0123456789abcdefghij0123456789"
	full := "usb-239a-8029-" + serial
	want := full[:54] + "-" + hex.EncodeToString(sumSHA256(full))[:8]

	d := discoverRAK(t, func(fsys fstest.MapFS) { withFile(fsys, "5-1/serial", serial) })
	if d.Name != want {
		t.Errorf("Name = %q, want %q", d.Name, want)
	}
	if len(d.Name) != 63 {
		t.Errorf("len(Name) = %d, want 63", len(d.Name))
	}
}

func sumSHA256(s string) []byte {
	sum := sha256.Sum256([]byte(s))
	return sum[:]
}
