# MicroVM Runtime

Goal: stronger isolation than containers for untrusted agent Cells.

## Why not fake it

A stub that returns success without a VMM would be **security theater**. Selecting `CELL_RUNTIME=microvm` stays **fail-closed** (`ErrNotImplemented` / validation errors) until a real `Backend` exists.

## Package shape (landed)

| Piece | Role |
|-------|------|
| `Runtime` | Implements `runtime.Runtime`; delegates to `Backend` |
| `Backend` | VMM-specific Boot/Exec/Halt |
| `Config` | `CELL_MICROVM_*` paths; `Validate()` before boot |
| `runtime.ParseName` | `CELL_RUNTIME=docker\|host\|microvm` |

## Planned Backend

- Firecracker (preferred), Cloud Hypervisor, or QEMU microVM
- Same control plane / Worker / SDK contracts — only `celld` runtime wiring changes
- Rootfs + kernel under `~/.cell/runtime/microvm/` (or `CELL_MICROVM_DIR`)
- Networking: vsock or tap with the same egress policies (`none` / `default` / `allowlist` via host-side proxy)
- Lifecycle: Create boots VM, Exec via guest agent or vsock, Destroy SIGKILL + reap

## Milestones

| ID | Deliverable |
|----|-------------|
| M0 | Fail-closed Runtime + Config + `CELL_RUNTIME=microvm` wiring (this PR) |
| M1 | Firecracker Backend boots pinned kernel/rootfs; Exec via serial/vsock agent |
| M2 | Egress parity (`none` / allowlist host proxy); jailer / non-root where possible |
| M3 | Artifact build pipeline + CI job (nested virt or dedicated runner) |

## Prerequisites (M1+)

- VMM binary on PATH (`CELL_MICROVM_VMM`)
- Guest kernel + rootfs (`CELL_MICROVM_KERNEL`, `CELL_MICROVM_ROOTFS`)
- Non-root VMM where possible (jailer)

Track status in `TECH_DEBT.md`.
