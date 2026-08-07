# rtlsdr-dra

A Kubernetes [DRA](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/)
driver that exposes RTL-SDR dongles the way a GPU is exposed: as a pool of schedulable
devices you request, not a per-serial resource name you hardcode.

```yaml
# any free radio
requests:
  - name: radio
    exactly: { deviceClassName: rtl-sdr.jacaudi.dev, allocationMode: ExactCount, count: 1 }

# ...or one specific stick
            selectors:
              - cel:
                  expression: device.attributes["sdr.jacaudi.dev"].serial == "35650102"
```

## Why not a device plugin

The classic device-plugin API has no selector: a pod asks for a *count* of a resource
*name* and the kubelet picks the IDs. The only way to pin a particular dongle is to
give it its own resource name (`devic.es/rtlsdr-acars`, `devic.es/rtlsdr-vdl2`), which
means every serial is baked into cluster config.

DRA publishes the serial as a device **attribute** instead, so one driver serves both
cases: unpinned claims draw from the pool, and a CEL selector pins a specific radio.

## How it works

1. A DaemonSet scans `/sys/bus/usb/devices` on every node for `0bda:2838` dongles.
2. Each dongle becomes one device in that node's pool, named `rtlsdr-<serial>`, with
   `serial`, `vendor`, `product` (and `manufacturer`/`productName` when reported) as
   attributes.
3. On `NodePrepareResources` the driver writes a CDI spec mapping the dongle's
   character device into the container and injects `RTLSDR_SERIAL`, so a container can
   target its own radio (`rtl_tcp -d "$RTLSDR_SERIAL"`).

### Two deliberate design choices

**The device node is never published as an attribute.** Bus and device numbers change
on every re-enumeration, so a claim written against them would silently select the
wrong radio after a replug. Only stable identity is published; the node is resolved
fresh from the serial at prepare time.

**A node with no dongles still publishes an empty pool.** "Pool with no devices" tells
the scheduler the driver is healthy and simply has no radios, which is different from
"no pool at all", and it avoids leaving a stale slice when the last dongle is pulled.

## Install

```sh
kubectl apply -f deploy/daemonset.yaml
kubectl apply -f deploy/claims.yaml
kubectl get resourceslices -l ''   # one pool per node
```

Requires Kubernetes ≥ 1.34 (`resource.k8s.io/v1`) with DRA enabled on the kubelet.

## Duplicate serials

RTL-SDR sticks ship with the factory serial `00000001`. Two un-reprogrammed dongles on
one host are ambiguous — a pinned claim cannot tell them apart. The driver logs an
error naming the serial; fix it with `rtl_eeprom -s <new-serial>` and **physically
replug** (the EEPROM is only re-read at power-up; re-enumeration alone is not enough).

## Status

Verified end-to-end on a kind cluster running Kubernetes v1.34.0, against a synthetic
sysfs tree mirroring a real node and `mknod`-ed USB device nodes:

| Behaviour | Result |
|---|---|
| Publishes a ResourceSlice with the serial as an attribute | 2 devices, correct serials |
| Unpinned claim draws any free radio | got `rtlsdr-35650102` |
| CEL selector pins one specific stick | got `rtlsdr-37728719`, as selected |
| CDI injects only that dongle's node | `189,4` and `189,132` respectively, one per container |
| `RTLSDR_SERIAL` / `RTLSDR_DEVNODE` injected | present in both containers |
| Pool exhaustion | 3rd pod stayed Pending: "cannot allocate all claims" |
| Release on pod deletion | freed radio was reallocated to the waiting pod; CDI spec removed |

Unit tests (27) cover discovery, attribute publishing, CDI spec generation, and the
prepare/unprepare paths including re-enumeration and unplug.

Not yet exercised: real RTL-SDR hardware (the dongles were simulated via sysfs and
`mknod`), and multi-node scheduling.
