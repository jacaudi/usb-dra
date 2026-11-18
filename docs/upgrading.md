# Upgrading

This guide covers the two migrations that need manual steps: from the old
`rtlsdr-dra` driver, and from the hand-written manifests to the Helm chart.

## From rtlsdr-dra

Installing the chart does not remove the old driver's objects. Delete them by
hand:

- the `rtlsdr-dra` DaemonSet, ServiceAccount, ClusterRole and
  ClusterRoleBinding;
- the `sdr.jacaudi.dev` ResourceSlices;
- the old DeviceClass `rtl-sdr.jacaudi.dev`;
- `/var/lib/kubelet/plugins/sdr.jacaudi.dev` on each node.

Claims written against `sdr.jacaudi.dev` attributes or the old class must be
rewritten for the `usb.jacaudi.dev` attributes and the `rtl-sdr.usb.jacaudi.dev`
class.

The injected environment variables were renamed: `RTLSDR_SERIAL` and
`RTLSDR_DEVNODE` are now `USB_SERIAL` and `USB_DEVNODE`; update any container
that reads them.

## From the manifests to the chart

Earlier builds installed from hand-written DaemonSet and DeviceClass manifests
under `deploy/`. The chart uses the same object names but different pod
selector labels, and a DaemonSet selector cannot be changed in place, so
**delete the old DaemonSet first**. Helm also refuses to take over objects it
did not create, so delete the rest of what those manifests created too, then
install:

```sh
kubectl -n kube-system delete daemonset,serviceaccount usb-dra
kubectl delete clusterrole,clusterrolebinding usb-dra
kubectl delete validatingadmissionpolicy,validatingadmissionpolicybinding usb-dra-resourceslices
kubectl delete deviceclass rtl-sdr.usb.jacaudi.dev rak4631.usb.jacaudi.dev
helm install usb-dra oci://ghcr.io/jacaudi/charts/usb-dra -n kube-system -f examples/values.yaml
```

The same delete list applies to an install made from `install.yaml`. Those
objects carry `app.kubernetes.io/managed-by: Helm` but no
`meta.helm.sh/release-*` annotations, so `helm install` refuses to adopt them.

[← Back to README](../README.md)
