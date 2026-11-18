# Selecting devices

This guide shows how a claim asks for any free device or pins one specific
device, and how to write a CEL selector that is safe.

An **unpinned** claim draws any free device of its DeviceClass. A **CEL
selector** on the request pins one specific device by serial or by port. A
device is allocated to at most one claim at a time. Use a ResourceClaimTemplate
so each pod gets its own claim; the scheduler places the pod on the node that
holds the allocated device.

## Writing a safe selector

Two things make a CEL selector safe or not:

- A reference to an attribute a device lacks aborts allocation ("no such key"),
  and class selectors are evaluated against devices of every driver.
- So: every DeviceClass selector starts with
  `device.driver == "usb.jacaudi.dev"` joined to the rest with `&&`; pin by
  serial with `identity == "serial" && serial == "<serial>"`; pin by port with
  `port == "<port>"` (`port` is always present); and reference `serial`,
  `manufacturer`, `productName` and `interfaceClasses` only behind
  `identity == "serial"` (for `serial`) or `has(...)`.

CEL has no alias for the attribute map, so selectors spell it out. The chart
renders the DeviceClasses from its `deviceClasses` values; the example
deployment in `examples/values.yaml` defines the two used below (see
[Installing](install.md)), and the example claims are in `examples/claims.yaml`
(the CUPS printer claim template is in
`examples/helmrelease-usb-printer.yaml`).
The attributes are listed in the [attribute reference](schema/attributes.md).

## Examples

Any RTL-SDR (the DeviceClass is the `rtl-sdr` class from `examples/values.yaml`;
the claim template is in `examples/claims.yaml`):

```yaml
apiVersion: resource.k8s.io/v1
kind: DeviceClass
metadata:
  name: rtl-sdr.usb.jacaudi.dev
spec:
  selectors:
    - cel:
        expression: >-
          device.driver == "usb.jacaudi.dev" &&
          device.attributes["usb.jacaudi.dev"].vendor == "0bda" &&
          device.attributes["usb.jacaudi.dev"].product in ["2838", "2832"]
---
apiVersion: resource.k8s.io/v1
kind: ResourceClaimTemplate
metadata:
  name: rtl-sdr-any
  namespace: selfhosted
spec:
  spec:
    devices:
      requests:
        - name: radio
          exactly:
            deviceClassName: rtl-sdr.usb.jacaudi.dev
            allocationMode: ExactCount
            count: 1
```

One RTL-SDR pinned by serial:

```yaml
apiVersion: resource.k8s.io/v1
kind: ResourceClaimTemplate
metadata:
  name: rtl-sdr-35650102
  namespace: selfhosted
spec:
  spec:
    devices:
      requests:
        - name: radio
          exactly:
            deviceClassName: rtl-sdr.usb.jacaudi.dev
            allocationMode: ExactCount
            count: 1
            selectors:
              - cel:
                  expression: >-
                    device.attributes["usb.jacaudi.dev"].identity == "serial" &&
                    device.attributes["usb.jacaudi.dev"].serial == "35650102"
```

The RAK4631 LoRa board pinned by port (it has a usable serial, but the port pin
also works for devices that have none). The DeviceClass is the `rak4631` class
from `examples/values.yaml`:

```yaml
apiVersion: resource.k8s.io/v1
kind: DeviceClass
metadata:
  name: rak4631.usb.jacaudi.dev
spec:
  selectors:
    - cel:
        expression: >-
          device.driver == "usb.jacaudi.dev" &&
          device.attributes["usb.jacaudi.dev"].vendor == "239a" &&
          device.attributes["usb.jacaudi.dev"].product in ["8029"]
---
apiVersion: resource.k8s.io/v1
kind: ResourceClaimTemplate
metadata:
  name: rak4631-lora-port
  namespace: selfhosted
spec:
  spec:
    devices:
      requests:
        - name: lora
          exactly:
            deviceClassName: rak4631.usb.jacaudi.dev
            allocationMode: ExactCount
            count: 1
            selectors:
              - cel:
                  expression: device.attributes["usb.jacaudi.dev"].port == "PNP0D10:02-v2-1"
```

There is deliberately no match-all DeviceClass: it would let any namespace claim
any published device. The chart creates no DeviceClass on its own; add one class
per device family you want to hand out, through the chart's `deviceClasses`
values (see [Adding a DeviceClass](configuration.md#adding-a-deviceclass)).

[← Back to README](../README.md)
