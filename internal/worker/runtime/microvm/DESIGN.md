# MicroVM Runtime

Goal: stronger isolation than containers for untrusted agent Cells.

## Why not fake it

Selecting `CELL_RUNTIME=microvm` never silently falls back to Docker/host. Missing VMM, kernel, rootfs, or guest agent → hard error.

## Package shape

| Piece | Role |
|-------|------|
| `Runtime` | Implements `runtime.Runtime`; delegates to `Backend` |
| `Firecracker` | Backend — API sock, vsock agent, M2 net + jailer |
| `Config` | `CELL_MICROVM_*` paths; `ResolveDefaults` + `Validate` |
| `guestproto` | Length-prefixed JSON over vsock |
| `cmd/cell-guest-agent` | Guest init/agent (ping / sync_workspace / exec) |

## Boot flow

1. `scripts/fetch-microvm-artifacts.sh` → `~/.cell/runtime/microvm/{firecracker,vmlinux}` (+ jailer)
2. Ultra-tiny `rootfs.ext4` via `scripts/build-microvm-rootfs.sh` (agent + busybox, ~6MiB, no journal); mounted **read-only**; guest agent tmpfs-mounts `/workspace` `/tmp` `/etc`
3. Network policy: `none` (no NIC) or `allowlist` (TAP + host `cell-egress` + DNS on TAP IP:53); `default`/`open` rejected
4. VMM starts via **jailer** unless `CELL_MICROVM_ALLOW_UNJAILED=1` (non-prod)
5. Configure machine, rootfs, optional eth0, vsock → `InstanceStart` (allowlist adds `cell.nameserver=<hostIP>`)
6. Guest agent seeds `/etc/resolv.conf` from cmdline; host also applies via exec after ping
7. Host waits for agent `ping`, tar.gz-syncs workspace over vsock
8. `Exec` merges allowlist `HTTP_PROXY*` into guest env; `Destroy` tears down TAP/proxy/DNS

## Milestones

| ID | Deliverable | Status |
|----|-------------|--------|
| M0 | Fail-closed Runtime + Config + `CELL_RUNTIME=microvm` | done |
| M1 | Firecracker Backend + vsock guest agent + artifact fetch script | done |
| M2 | Egress parity (`none` / allowlist host proxy + DNS); jailer / non-root | done |
| M3 | Ultra-tiny rootfs (`scripts/build-microvm-rootfs.sh`, RO root + tmpfs) + CI | done |

## Env

| Variable | Default |
|----------|---------|
| `CELL_MICROVM_DIR` | `~/.cell/runtime/microvm` |
| `CELL_MICROVM_VMM` | `firecracker` |
| `CELL_MICROVM_JAILER` | `jailer` (required unless unjailed) |
| `CELL_MICROVM_ALLOW_UNJAILED` | unset — set `1` only for CI/dev without jailer |
| `CELL_MICROVM_KERNEL` | `$DIR/vmlinux` |
| `CELL_MICROVM_ROOTFS` | `$DIR/rootfs.ext4` |
| `CELL_MICROVM_MEM_MIB` | `256` |
| `CELL_MICROVM_VCPUS` | `1` |
| `CELL_MICROVM_VSOCK_PORT` | `5032` |

Requires `/dev/kvm`, Firecracker-compatible kernel/rootfs, and (for allowlist) `CAP_NET_ADMIN` (TAP) plus bind on `:53` for the DNS forwarder.

Optional rootfs knobs: `CELL_MICROVM_ROOTFS_MB` (fixed size), `CELL_BUSYBOX_BIN` (static busybox), `CELL_RG_BIN` / `CELL_RG_VERSION` / `CELL_MICROVM_SKIP_RG=1`. Guest ships agent applets (`grep`/`find`/`sed`/`awk`/…) plus static `rg`. CI builds rootfs and fails if the image exceeds 32MiB.
