package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jacaudi/usb-dra/internal/usb"
)

// loadFixture reads internal/usb/testdata/sysfs/<name>.tsv, one "path<TAB>content" line per sysfs file.
func loadFixture(t *testing.T, name string) fstest.MapFS {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "internal", "usb", "testdata", "sysfs", name+".tsv"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	fsys := fstest.MapFS{}
	for line := range strings.Lines(string(b)) {
		line = strings.TrimSuffix(line, "\n")
		if line == "" {
			continue
		}
		p, content, ok := strings.Cut(line, "\t")
		if !ok {
			t.Fatalf("fixture %s: line %q has no tab", name, line)
		}
		fsys[p] = &fstest.MapFile{Data: []byte(content + "\n")}
	}
	return fsys
}

// withFile sets the sysfs file rel (relative to usb.SysfsUSBDir) to content.
func withFile(fsys fstest.MapFS, rel, content string) {
	fsys[usb.SysfsUSBDir+"/"+rel] = &fstest.MapFile{Data: []byte(content + "\n")}
}

// without removes the sysfs file or directory rel (relative to usb.SysfsUSBDir) and everything under it.
func without(fsys fstest.MapFS, rel string) {
	key := usb.SysfsUSBDir + "/" + rel
	delete(fsys, key)
	for k := range fsys {
		if strings.HasPrefix(k, key+"/") {
			delete(fsys, k)
		}
	}
}
