package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
)

const (
	// checkpointFile is the checkpoint's file name inside the plugin data directory.
	checkpointFile = "checkpoint.json"

	// checkpointVersion is the only on-disk format this driver reads or writes.
	checkpointVersion = 1
)

// checkpointDevice is one device allocated to a claim, in allocation-result order.
type checkpointDevice struct {
	Device  string `json:"device"`
	Request string `json:"request"`
	Port    string `json:"port"`
}

// checkpointClaim is one prepared claim.
type checkpointClaim struct {
	Namespace string             `json:"namespace"`
	Name      string             `json:"name"`
	Devices   []checkpointDevice `json:"devices"`
}

// checkpoint is the prepared-claim state that survives a driver restart, keyed by claim UID.
type checkpoint struct {
	Version int                        `json:"version"`
	Claims  map[string]checkpointClaim `json:"claims"`
}

// loadCheckpoint reads the checkpoint at path; a missing file is an empty state.
func loadCheckpoint(path string) (checkpoint, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, fs.ErrNotExist) {
		return checkpoint{Version: checkpointVersion, Claims: map[string]checkpointClaim{}}, nil
	}
	if err != nil {
		return checkpoint{}, fmt.Errorf("read checkpoint %s: %w", path, err)
	}

	var c checkpoint
	if err := json.Unmarshal(raw, &c); err != nil {
		return checkpoint{}, fmt.Errorf("parse checkpoint %s (delete it to start empty): %w", path, err)
	}
	if c.Version != checkpointVersion {
		return checkpoint{}, fmt.Errorf("checkpoint %s has unsupported version %d, want %d (delete it to start empty)", path, c.Version, checkpointVersion)
	}
	if c.Claims == nil {
		c.Claims = map[string]checkpointClaim{}
	}
	return c, nil
}

// save writes the checkpoint to path atomically: temp file in the same directory, sync, rename.
func (c checkpoint) save(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encode checkpoint: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "checkpoint-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp checkpoint beside %s: %w", path, err)
	}

	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	err = errors.Join(err, tmp.Close())
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		return fmt.Errorf("save checkpoint %s: %w", path, errors.Join(err, os.Remove(tmp.Name())))
	}
	return nil
}

// with returns a copy of c that holds cl under uid; c is not modified.
func (c checkpoint) with(uid string, cl checkpointClaim) checkpoint {
	c = c.clone()
	c.Claims[uid] = cl
	return c
}

// without returns a copy of c with uid removed; c is not modified.
func (c checkpoint) without(uid string) checkpoint {
	c = c.clone()
	delete(c.Claims, uid)
	return c
}

// clone copies the claims map (c is a value receiver); a nil map becomes empty so writes cannot panic.
func (c checkpoint) clone() checkpoint {
	c.Claims = maps.Clone(c.Claims)
	if c.Claims == nil {
		c.Claims = map[string]checkpointClaim{}
	}
	return c
}

// holder returns the lexically smallest claim UID other than except that holds a device
// with this name or, when port is non-empty, this port.
func (c checkpoint) holder(device, port, except string) (uid string, ok bool) {
	for id, cl := range c.Claims {
		if id == except || (ok && id >= uid) {
			continue
		}
		for _, d := range cl.Devices {
			if d.Device == device || (port != "" && d.Port == port) {
				uid, ok = id, true
				break
			}
		}
	}
	return uid, ok
}
