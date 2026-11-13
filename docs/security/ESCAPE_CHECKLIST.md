# Docker escape checklist (baseline)

Run before promoting a worker host to internal production. Still **container** isolation — not a microVM guarantee.

## Daemon & host

- [ ] Docker daemon not exposing TCP without TLS
- [ ] Operator user in `docker` group is tightly controlled (group ≡ root)
- [ ] Host kernel reasonably current; reboot after urgent CVE patches
- [ ] No unnecessary privileged DaemonSets / hostPath mounts on the same node

## Cell runtime flags (verify in code / `docker inspect`)

- [ ] `--security-opt=no-new-privileges`
- [ ] `--cap-drop=ALL` (+ only documented additions)
- [ ] Read-only rootfs with explicit tmpfs/writable mounts
- [ ] Non-root user inside guest where templates allow
- [ ] `--cgroupns=private` (default path)
- [ ] Not forcing `--userns=host` unless `CELL_DOCKER_USERNS=host` is an explicit escape hatch

## Mounts & secrets

- [ ] No bind-mount of Docker socket into Cells
- [ ] No host credential paths (`~/.ssh`, cloud metadata cookies, kubeconfigs) into workspaces
- [ ] Git mirrors mounted read-only
- [ ] Package caches are per-Cell writable dirs (not a shared writable host cache)

## Network

- [ ] Untrusted templates use `network: none` or `allowlist`, not open `default`, when policy requires
- [ ] Allowlist Cells run `cell-egress`; audit log path writable only to operator
- [ ] Host firewall restricts worker RPC ports to control / mesh peers

## Auth

- [ ] `CELL_WORKER_TOKEN` set and high entropy in every environment beyond local demo
- [ ] Rotation tested with `CELL_WORKER_TOKEN_PREV`
- [ ] Unauthenticated `/worker/v1/*` returns 401

## Runtime mode

- [ ] Production workers do **not** set `CELL_RUNTIME=host`
- [ ] `CELL_RUNTIME=microvm` only with jailer on PATH (or documented `CELL_MICROVM_ALLOW_UNJAILED=1` for non-prod)
- [ ] MicroVM templates use `network: none` or `allowlist` (not `default`/`open`)
- [ ] Allowlist microVM workers have `CAP_NET_ADMIN` for TAP + host `cell-egress`

## Residual

- [ ] Owners accept container breakout residual risk **or** gate high-risk workloads until microVM
