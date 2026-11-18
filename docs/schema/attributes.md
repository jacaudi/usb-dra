# Device attributes

This reference lists every attribute the driver publishes on a device in its
ResourceSlice, which is the data a CEL selector can read.

All attributes are strings under `device.attributes["usb.jacaudi.dev"]`.

| Key | When | Bound | Example |
| --- | --- | --- | --- |
| `vendor` | always | 4 chars | `239a` |
| `product` | always | 4 chars | `8029` |
| `identity` | always | | `serial` or `port` |
| `port` | always; a device whose port exceeds 64 bytes is skipped and logged | 64 bytes | `PNP0D10:02-v2-1` |
| `serial` | when non-empty, valid UTF-8, at most 64 bytes, free of control characters and not all zeros (duplicates included) | 64 bytes | `FD14948954CD1F4F` |
| `manufacturer` | when reported and valid UTF-8; truncated on a rune boundary | 64 bytes | `RAKwireless` |
| `productName` | same as `manufacturer` | 64 bytes | `WisCore RAK4631 Board` |
| `interfaceClasses` | when at most 64 bytes; sorted unique, comma-joined | 64 bytes | `02,0a` |

`port` is `<controller>-v<revision>-<port chain>`: the controller is the root
hub's `serial` (the kernel sets it to the controller's bus name), the revision
is the USB generation derived from the bus speed, and the chain is the device's
path below the bus. The bus number is never part of it. A SuperSpeed device that
falls back to high speed changes revision and therefore port.

Never published: the device node, bus number, device number, tty names and tty
aliases. They change on every re-enumeration, so a claim written against them
could silently select the wrong device after a replug.

See [Selecting devices](../selecting-devices.md) for which attributes a
selector may reference safely, and [Device identity](../identity.md) for how
`identity` and `serial` are decided.

[← Back to README](../../README.md)
