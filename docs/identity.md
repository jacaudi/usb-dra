# Device identity

This page explains how the driver names a device and tells two devices apart,
and what happens when it cannot.

Every device gets a name `usb-<vid>-<pid>-<id>`, lowercased and sanitized to a
DNS label (a name over 63 characters is truncated and suffixed with a short
hash).

- **Serial identity.** The device has a usable serial: non-empty, valid UTF-8,
  at most 64 bytes, free of control characters, and not all zeros. `<id>` is the
  serial, for example `usb-0bda-2838-35650102`.
- **Port identity.** Everything else. `<id>` is `p-<port>`, for example
  `usb-1a86-7523-p-0000-00-14-0-v2-1-4`.

The `identity` attribute says which one a device has (`serial` or `port`); see
the [attribute reference](schema/attributes.md).

## Duplicate serials

If two devices end up with the same name (a shared serial, or serials that are
equal after case-folding or sanitizing), every one of them falls back to port
identity and the duplicate is logged as an error naming the serial. The
`serial` attribute is still published on them, but their `identity` is `port`,
so a claim pinned with the shipped serial idiom
(`identity == "serial" && serial == "..."`) stops matching either device rather
than matching one of them. An already prepared claim keeps its spec until its
next prepare, which then fails with "no longer attached". Fix the serials and
replug.

RTL-SDR sticks ship with the factory serial `00000001`. Reprogram one with
`rtl_eeprom -s <new-serial>` and **physically replug**; the EEPROM is only
re-read at power-up, so re-enumeration alone is not enough.

## Swapping port-identity devices

Swapping two identical port-identity devices between ports swaps their
identities with no error; the `identity` attribute exists so a claim author can
see this coming.

[← Back to README](../README.md)
