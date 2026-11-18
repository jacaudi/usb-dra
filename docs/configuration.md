# Configuration

This reference lists the chart values and shows how to add a DeviceClass. The
driver's own flags are in [Discovery](discovery.md#flags).

## Values

The chart's own `values.yaml` is empty (`{}`): every default below lives in the
templates, so `examples/values.yaml` lists only what a deployment chooses. A
value that is set is used as given, even when empty or zero.

| Value | Default | Meaning |
| --- | --- | --- |
| `image.repository` | `ghcr.io/jacaudi/usb-dra` | Image repository. |
| `image.tag` | `v` plus the chart's `appVersion` | Used verbatim when set. |
| `image.pullPolicy` | `IfNotPresent` | `Always`, `IfNotPresent` or `Never`. |
| `driver.excludeClasses` | not passed; the binary's `03,08,e0` applies | `--exclude-classes`, see [Discovery](discovery.md#what-is-published). |
| `driver.include` | none | One `--include` per entry, each `vid:pid` or `vid:pid:serial`. |
| `driver.rescanInterval` | not passed; the binary's `30s` applies | `--rescan-interval`, a Go duration. |
| `driver.verbosity` | `2` | `-v`, 0 to 10. |
| `driver.cdiRoot` | `/var/run/cdi` | `--cdi-root` and the host mount move together; change it when a node's containerd uses a different `cdi_spec_dirs`. |
| `deviceClasses` | none: no DeviceClass is created | Map of DeviceClasses, see below. |
| `resources` | 10m CPU / 32Mi memory request, 128Mi memory limit | Container resources. A set value replaces the whole default. |
| `nodeSelector` | none | Restrict the DaemonSet to some nodes. |
| `tolerations` | `- operator: Exists` | Every node by default: an empty pool means "healthy, no devices". A set value replaces it. |
| `priorityClassName` | `system-node-critical` | Pod priority class. |

The chart builds the DaemonSet from fixed templates and reads only the values
above, so a values edit cannot drop the things the driver cannot run without:
root, `privileged`, the host mounts, the RBAC, the driver name
`usb.jacaudi.dev`. Object names are the release name. The values schema rejects
unknown keys, name overrides and malformed IDs at render time instead of
ignoring them.

## Adding a DeviceClass

A device type is a values entry, not a chart release. Start from
`examples/values.yaml`, which defines `rtl-sdr` and `rak4631`. The key becomes
the class name `<key>.usb.jacaudi.dev`; `vendor` and each product are four
lowercase hex digits and **must be quoted**, because an unquoted `1e03` or
`0403` is a YAML number and the schema rejects it.

```yaml
# my-values.yaml, layered over the example: -f examples/values.yaml -f my-values.yaml
deviceClasses:
  ch340: # DeviceClass ch340.usb.jacaudi.dev
    vendor: "1a86"
    products:
      - "7523"
  rak4631: null # drop a class the example defines
```

```sh
helm upgrade --install usb-dra oci://ghcr.io/jacaudi/charts/usb-dra -n kube-system \
  -f examples/values.yaml -f my-values.yaml
```

Each class selects this driver's devices of that vendor and one of those
products, with the `device.driver` guard every shipped selector needs.

[← Back to README](../README.md)
