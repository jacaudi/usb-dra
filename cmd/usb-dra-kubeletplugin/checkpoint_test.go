package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	testCheckpointDevice = "usb-239a-8029-fd14948954cd1f4f"
	testCheckpointPort   = "PNP0D10:02-v2-1"
)

func testCheckpointClaim() checkpointClaim {
	return checkpointClaim{
		Namespace: "selfhosted",
		Name:      "lora",
		Devices:   []checkpointDevice{{Device: testCheckpointDevice, Request: "lora", Port: testCheckpointPort}},
	}
}

func writeCheckpointFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), checkpointFile)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write checkpoint file: %v", err)
	}
	return path
}

func TestLoadCheckpointMissingIsEmpty(t *testing.T) {
	got, err := loadCheckpoint(filepath.Join(t.TempDir(), checkpointFile))
	if err != nil {
		t.Fatalf("loadCheckpoint: %v", err)
	}
	if got.Version != checkpointVersion || len(got.Claims) != 0 {
		t.Fatalf("got %+v, want version %d and no claims", got, checkpointVersion)
	}
}

func TestLoadCheckpointNullClaims(t *testing.T) {
	path := writeCheckpointFile(t, `{"version":1,"claims":null}`)

	got, err := loadCheckpoint(path)
	if err != nil {
		t.Fatalf("loadCheckpoint: %v", err)
	}
	if got.Claims == nil || len(got.Claims) != 0 {
		t.Fatalf("got claims %#v, want a non-nil empty map", got.Claims)
	}
}

func TestCheckpointRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), checkpointFile)
	want := checkpoint{Version: checkpointVersion, Claims: map[string]checkpointClaim{}}.with("uid-a", testCheckpointClaim())

	if err := want.save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := loadCheckpoint(path)
	if err != nil {
		t.Fatalf("loadCheckpoint: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(string(raw), `"version": 1`) {
		t.Fatalf("file lacks %q:\n%s", `"version": 1`, raw)
	}
}

func TestCheckpointSaveIsAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, checkpointFile)
	c := checkpoint{Version: checkpointVersion, Claims: map[string]checkpointClaim{}}.with("uid-a", testCheckpointClaim())

	// Saving twice also covers renaming over an existing file.
	for range 2 {
		if err := c.save(path); err != nil {
			t.Fatalf("save: %v", err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != checkpointFile {
		t.Fatalf("dir holds %v, want exactly %s", entries, checkpointFile)
	}
}

func TestCheckpointSaveFailureLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	// A directory at the target makes Rename fail after the temp file is written.
	path := filepath.Join(dir, checkpointFile)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	c := checkpoint{Version: checkpointVersion, Claims: map[string]checkpointClaim{}}

	if err := c.save(path); err == nil {
		t.Fatal("save over a directory: want error, got nil")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != checkpointFile {
		t.Fatalf("dir holds %v, want only the directory %s", entries, checkpointFile)
	}
}

func TestCheckpointCopyOnWrite(t *testing.T) {
	c := checkpoint{Version: checkpointVersion, Claims: map[string]checkpointClaim{}}.with("uid-a", testCheckpointClaim())

	added := c.with("uid-b", testCheckpointClaim())
	if len(c.Claims) != 1 || len(added.Claims) != 2 {
		t.Fatalf("with: receiver has %d claims, copy has %d; want 1 and 2", len(c.Claims), len(added.Claims))
	}

	removed := c.without("uid-a")
	if len(c.Claims) != 1 || len(removed.Claims) != 0 {
		t.Fatalf("without: receiver has %d claims, copy has %d; want 1 and 0", len(c.Claims), len(removed.Claims))
	}
}

func TestLoadCheckpointRejectsUnknownVersion(t *testing.T) {
	path := writeCheckpointFile(t, `{"version": 2, "claims": {}}`)

	_, err := loadCheckpoint(path)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	for _, want := range []string{"version 2", path} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}

func TestLoadCheckpointRejectsGarbage(t *testing.T) {
	path := writeCheckpointFile(t, "not json")

	_, err := loadCheckpoint(path)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q lacks the file path %q", err, path)
	}
}

func TestCheckpointHolder(t *testing.T) {
	c := checkpoint{Version: checkpointVersion, Claims: map[string]checkpointClaim{}}.
		with("uid-a", testCheckpointClaim())

	tests := []struct {
		name         string
		c            checkpoint
		device, port string
		except       string
		wantUID      string
		wantOK       bool
	}{
		{"same device name", c, testCheckpointDevice, "", "uid-z", "uid-a", true},
		{"same port, other name", c, "other-name", testCheckpointPort, "uid-z", "uid-a", true},
		{"holder is the excepted claim", c, testCheckpointDevice, "", "uid-a", "", false},
		{"no match", c, "x", "y", "", "", false},
		{"empty port matches no device", c.with("uid-b", checkpointClaim{Devices: []checkpointDevice{{Device: "n"}}}), "x", "", "", "", false},
		{
			"two holders: device name from uid-a, port from uid-0",
			c.with("uid-0", checkpointClaim{Devices: []checkpointDevice{{Device: "p", Port: "P"}}}).
				with("uid-a", checkpointClaim{Devices: []checkpointDevice{{Device: "N", Port: "q"}}}),
			"N", "P", "uid-a", "uid-0", true,
		},
		{
			"two holders: lexically smallest uid wins",
			c.with("uid-0", checkpointClaim{Devices: []checkpointDevice{{Device: "p", Port: "P"}}}).
				with("uid-a", checkpointClaim{Devices: []checkpointDevice{{Device: "N", Port: "q"}}}),
			"N", "P", "uid-z", "uid-0", true,
		},
		{
			"two holders, other side",
			c.with("uid-0", checkpointClaim{Devices: []checkpointDevice{{Device: "p", Port: "P"}}}).
				with("uid-a", checkpointClaim{Devices: []checkpointDevice{{Device: "N", Port: "q"}}}),
			"N", "P", "uid-0", "uid-a", true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotUID, gotOK := tt.c.holder(tt.device, tt.port, tt.except)
			if gotUID != tt.wantUID || gotOK != tt.wantOK {
				t.Fatalf("holder(%q, %q, %q) = (%q, %v), want (%q, %v)",
					tt.device, tt.port, tt.except, gotUID, gotOK, tt.wantUID, tt.wantOK)
			}
		})
	}
}
