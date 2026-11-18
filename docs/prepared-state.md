# Prepared state

This page explains how the driver records prepared claims so they survive a
restart, and how to recover when the record is damaged.

The driver records every prepared claim in `checkpoint.json` in its plugin data
directory, `/var/lib/kubelet/plugins/usb.jacaudi.dev/checkpoint.json`. It is
written atomically (temp file, sync, rename) after the claim's CDI spec on
prepare and after the spec is removed on unprepare, and loaded once at startup
before the plugin registers, so prepared state survives a driver restart.

## Prepare

Each device entry records the device name, the request, and the port. On
prepare, the driver resolves each allocated device by name from a fresh sysfs
scan, then refuses any device that another claim already holds in the
checkpoint, matching on device name, or on port (so a device that comes back
under a new name on the same port is still caught) unless the holder's device is
attached on a different port now. Re-preparing a known claim rewrites its CDI
spec, which rebuilds the spec after a node reboot empties `/var/run/cdi`.
Allocation results with `adminAccess` are rejected, because two processes
sharing a USB device or tty corrupt each other. Results from a pool other than
this node's pool are rejected too.

## Unprepare

Unprepare always removes the spec and checkpoint entry by claim UID, and
succeeds for an unknown claim.

## Recovery

A missing checkpoint is an empty state. An unreadable, unparseable or
unknown-version file stops the driver at startup with an error naming the file.
To recover, delete the file and restart the pod; the kubelet's own state still
drives unprepare.

[← Back to README](../README.md)
