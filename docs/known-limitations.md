# Known limitations

This reference lists the situations where the driver cannot keep a device
assignment correct, and what to do about each.

- **Running containers keep their device nodes.** runc creates nodes by
  major:minor, and tty minors are reused lowest-free. If a board re-enumerates
  while its container runs, that container's `/dev/ttyACM0` can end up pointing
  at another board. The driver re-resolves only at prepare. The upstream signals
  for this (resource health, device taints) are alpha and disabled on the 1.35
  cluster.
- **Container restarts reuse the prepared spec.** The kubelet does not
  re-prepare for a container restart, so a restarted container gets the paths
  resolved at the last prepare.
- **Port-identity swaps.** Two identical devices without usable serials swap
  identities if they swap ports.
- **Moved while claimed.** A port-identity device moved to another port while
  claimed is a new device to the driver and can be prepared for a second claim.
- **tty alias collisions.** Two claims in one container that reuse a request
  name collide on `/dev/dra/<request>/`; the runtime applies the last one
  without error. Use distinct request names.
- **Env merging.** A claim holding two devices from this driver gets their
  `USB_*` variables merged per key.

See [Device identity](identity.md) for the identity rules behind the swap cases
and [ttys and aliases](ttys-and-aliases.md) for the injected paths.

[← Back to README](../README.md)
