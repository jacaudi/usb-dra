# Discovery

This reference explains which USB devices the driver publishes and the flags
that tune the filter.

## What is published

A DaemonSet scans `/sys/bus/usb/devices` on every node and publishes one device
per USB device, except:

- **Hubs.** Any device with `bDeviceClass` `09`, root hubs included. No flag
  overrides this.
- **USB/IP devices.** Anything behind a `vhci_hcd` controller.
- **Excluded classes.** A device is hidden when every one of its interface
  classes is in the excluded set. The default is `03,08,e0` (HID, mass storage,
  wireless controller), so a keyboard or a Bluetooth dongle is never claimable
  by accident. Only interface classes count, never `bDeviceClass`: real devices
  report `00` or `ef` there.
- **Unconfigured devices** with no interface entries, and devices whose
  attribute files cannot be read mid-enumeration, or whose root hub speed is
  unknown. These are skipped for that scan and logged at `-v=2`; an entry
  missing one of its identity files is dropped silently.

## Flags

Two flags tune the filter:

| Flag | Default | Meaning |
| --- | --- | --- |
| `--exclude-classes` | `03,08,e0` | Comma-separated interface classes, each two lowercase hex digits. Replaces the default; an empty value publishes everything except hubs and vhci devices. |
| `--include` | none | Repeatable `vid:pid` or `vid:pid:serial` (four lowercase hex digits each; the serial may contain `:` and compares case-insensitively). A matching device is published whatever its interface classes. |

An invalid value for either flag is a startup error. The other flags are
`--driver-name`, `--node-name` (default `$NODE_NAME`), `--cdi-root`,
`--kubelet-registrar-dir`, `--kubelet-plugin-dir`, `--sysfs-root` and
`--rescan-interval`; see `--help`. The chart sets the filter flags through
[`driver.excludeClasses` and `driver.include`](configuration.md#values).

## Limits and the empty pool

A node publishes at most 128 devices (the ResourceSlice limit); any beyond that
are not published and an error is logged.

A node with no eligible devices still publishes an empty pool. "Pool with no
devices" tells the scheduler the driver is healthy and has nothing to offer,
which differs from "no pool at all", and it avoids leaving a stale slice behind
when the last device is unplugged.

[← Back to README](../README.md)
