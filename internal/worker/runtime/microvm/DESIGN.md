# MicroVM Runtime (not implemented)

Goal: stronger isolation than containers for untrusted agent Cells.

## Why not fake it

A stub that returns success without a VMM would be **security theater**. This package stays fail-closed (`ErrNotImplemented`) until a real backend exists.

## Planned shape

- Implement `runtime.Runtime` using Firecracker, Cloud Hypervisor, or QEMU microVM.
- Same control plane / Worker / SDK contracts — only `celld` wiring changes.
- Rootfs + kernel artifacts versioned under `~/.cell/runtime/microvm/` (or image store).
- Networking: vsock or tap with the same egress policies (`none` / `default` / `allowlist` via host-side proxy).
- Lifecycle: Create boots VM, Exec via agent inside guest or vsock exec, Destroy SIGKILL + reap.

## Prerequisites

- VMM binary on PATH (e.g. `firecracker`)
- Guest kernel + rootfs build pipeline
- Non-root VMM where possible (jailer)

Track status in repo `TECH_DEBT.md`.
