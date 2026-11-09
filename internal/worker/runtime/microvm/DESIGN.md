# MicroVM Runtime

Goal: stronger isolation than containers for untrusted agent Cells.

## Why not fake it

Selecting `CELL_RUNTIME=microvm` never silently falls back to Docker/host. Missing VMM, kernel, rootfs, or guest agent → hard error.

## Package shape

| Piece | Role |
|-------|------|
| `Runtime` | Implements `runtime.Runtime`; delegates to `Backend` |
| `Firecracker` | M1 Backend — Firecracker API sock, vsock agent exec |
| `Config` | `CELL_MICROVM_*` paths; `ResolveDefaults` + `Validate` |
| `guestproto` | Length-prefixed JSON over vsock |
| `cmd/cell-guest-agent` | Guest init/agent (ping / sync_workspace / exec) |

## M1 flow

1. `scripts/fetch-microvm-artifacts.sh` → `~/.cell/runtime/microvm/{firecracker,vmlinux}`
2. Operator supplies `rootfs.ext4` with `/usr/local/bin/cell-guest-agent` (default `init=`)
3. `CELL_RUNTIME=microvm` → `Firecracker.Boot` configures machine, rootfs, vsock, `InstanceStart`
4. Host waits for agent `ping`, then tar.gz-syncs the Cell workspace over vsock
5. `Exec` → agent `sh -c` in `/workspace`; `Destroy` → shutdown + kill VMM

## Milestones

| ID | Deliverable | Status |
|----|-------------|--------|
| M0 | Fail-closed Runtime + Config + `CELL_RUNTIME=microvm` | done |
| M1 | Firecracker Backend + vsock guest agent + artifact fetch script | this PR |
| M2 | Egress parity (`none` / allowlist host proxy); jailer / non-root | open |
| M3 | Rootfs build pipeline in CI + nested-virt integration job | open |

## Env

| Variable | Default |
|----------|---------|
| `CELL_MICROVM_DIR` | `~/.cell/runtime/microvm` |
| `CELL_MICROVM_VMM` | `firecracker` |
| `CELL_MICROVM_KERNEL` | `$DIR/vmlinux` |
| `CELL_MICROVM_ROOTFS` | `$DIR/rootfs.ext4` |
| `CELL_MICROVM_MEM_MIB` | `256` |
| `CELL_MICROVM_VCPUS` | `1` |
| `CELL_MICROVM_VSOCK_PORT` | `5032` |

Requires `/dev/kvm` and a Firecracker-compatible kernel/rootfs on the worker.
