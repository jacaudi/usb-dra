# ttys and aliases

This reference describes what the driver injects into a container for each
allocated device: device nodes, stable tty aliases and environment variables.

On prepare, the driver maps the device into the container through a per-claim
CDI spec:

- the USB node `/dev/bus/usb/BBB/DDD`;
- every tty under the device's interfaces (`ttyACM*` from `cdc_acm`, `ttyUSB*`
  from usb-serial drivers), interfaces ordered by `bInterfaceNumber` and ttys
  within an interface by numeric suffix, each injected at `/dev/<name>`;
- each tty again at a stable alias `/dev/dra/<request>/tty<k>`. `<request>` is
  the claim request name (the part before `/` for a prioritized-list
  subrequest); `<k>` counts from 0 across every tty of every device allocated to
  that request. A RAK4631 claimed by a request named `lora` appears at
  `/dev/dra/lora/tty0`.

Device nodes carry only a host path, a container path and `rw` permissions.
Major, minor, UID, GID and file mode are left for the runtime to fill from the
host node, which keeps injection correct across re-enumeration.

## Environment variables

Per device:

| Variable | Value |
| --- | --- |
| `USB_DEVNODE` | The USB node, `/dev/bus/usb/BBB/DDD`. |
| `USB_SERIAL` | The serial; set for serial-identity devices only. |
| `USB_TTY` | The host path of the device's first tty, when it has one. |

A claim holding two devices from this driver gets their `USB_*` variables merged
per key. Kubernetes `$(VAR)` expansion does not see CDI environment variables,
which is why the alias path exists: point your application at
`/dev/dra/<request>/tty0`.

## Devices with no tty

A device with no bound tty is injected with its USB node only; prepare does not
fail. `ttyUSB*` needs the usb-serial driver bound. On Talos 1.12 `cdc_acm` is
built in; `ch341`, `cp210x`, `ftdi_sio` and `pl2303` are modules that udev
should autoload by modalias, which has not been exercised on real hardware. If
the tty does not appear, load the module via Talos `machine.kernel.modules`.

[← Back to README](../README.md)
