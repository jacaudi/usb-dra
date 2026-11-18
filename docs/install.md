# Installing usb-dra

This guide covers installing, checking and removing usb-dra on a cluster, with
Helm or without it. For the short version, see the
[Quick start](../README.md#quick-start).

## Install with Helm

```sh
helm install usb-dra oci://ghcr.io/jacaudi/charts/usb-dra -n kube-system -f examples/values.yaml
kubectl apply -f examples/claims.yaml
```

Run it from a checkout of this repository, or copy `examples/values.yaml` and
point `-f` at the copy. A plain `helm install` without `-f` deploys the driver
but **creates no DeviceClasses**: the chart does not decide which devices to
hand out, and a claim cannot select a device until a class exists.
`examples/values.yaml` defines `rtl-sdr` and `rak4631`; see
[Adding a DeviceClass](configuration.md#adding-a-deviceclass).

The chart version equals the app version without the `v`: pin one with the bare
semver, for example `--version 0.1.0`. Check that the driver is publishing:

```sh
kubectl get resourceslices
kubectl get deviceclasses
```

Each node should show one `usb.jacaudi.dev` pool, empty when the node has no
eligible devices.

`examples/claims.yaml` is an example: it targets the namespace `selfhosted`, so
edit it to match yours. It is not part of the chart.

Do not also expose these devices through generic-device-plugin: both would
allocate the same board.

Requires Kubernetes 1.34 or later (`resource.k8s.io/v1`) with DRA enabled on
the kubelet.

### With Flux

`examples/helmrelease-rtl-sdr.yaml` and `examples/helmrelease-usb-printer.yaml`
are complete Flux HelmReleases that pull the OCI chart inline
(`.spec.chart.kind: OCIRepository`). The printer example also carries a claim
template for a CUPS pod; its vendor and product IDs are placeholders to replace
with your printer's. Keep the HelmRelease itself in `kube-system`, with no
`targetNamespace`, so the release name stays `usb-dra`; Flux would otherwise
default it to `kube-system-usb-dra`, and changing a release name makes Flux
uninstall and reinstall. Run one release per cluster, so combine the classes you
need into one HelmRelease.

## Requirements to know before you install

- **Install into `kube-system`.** The DaemonSet runs as root and privileged on
  every node: the plugin socket and CDI directory are root-owned on the host. A
  namespace that enforces the baseline or restricted Pod Security level (Talos
  does outside `kube-system`) rejects the pods while `helm install` still
  reports success.
- **One release per cluster.** The driver name, the plugin socket on each node
  and the DeviceClass names are cluster-wide singletons.
- **Admission policy.** RBAC cannot scope ResourceSlice writes by node, so the
  chart also creates a ValidatingAdmissionPolicy: the driver's service account
  may write only its own node's `usb.jacaudi.dev` slices. It reads the node from
  the pod-bound service account token, which Kubernetes 1.34 includes by
  default. The policy is built from the release name and namespace, the same
  source as the service account, so they cannot drift apart.

## Install without Helm

Every release also publishes `install.yaml` on its GitHub Release: the chart
rendered with `examples/values.yaml` (so it includes the `rtl-sdr` and
`rak4631` DeviceClasses), release name `usb-dra`, namespace `kube-system` and
the release's image tag.

```sh
kubectl apply -f https://github.com/jacaudi/usb-dra/releases/download/v0.1.0/install.yaml
```

The release URL and `task artifact VERSION=` take the `v`-prefixed tag
(`v0.1.0`), unlike the chart's bare `--version 0.1.0`. Use Helm for any value
other than the example's. Render it yourself with
`task artifact VERSION=v0.1.0`.

## Uninstall

`helm uninstall usb-dra -n kube-system` removes the DaemonSet, RBAC, admission
policy and DeviceClasses. The driver's ResourceSlices outlive it; delete them
so the scheduler stops seeing devices nothing serves:

```sh
kubectl delete resourceslices --field-selector spec.driver=usb.jacaudi.dev
```

## Related

- [Configuration](configuration.md): every chart value and how to add a
  DeviceClass.
- [Upgrading](upgrading.md): moving from `rtlsdr-dra` or from the hand-written
  manifests.

[← Back to README](../README.md)
