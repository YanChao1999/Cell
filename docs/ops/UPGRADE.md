# Upgrade notes

## Version source

- Go binaries report `internal/version.Version` (`celld version` / `cell version`).
- Compose images rebuild from the Dockerfile; there is no separate image tag scheme yet beyond git SHA / local `:latest`.

## Rolling a compose cluster

1. Read [PLAYBOOK.md](PLAYBOOK.md) for drain/restart order.
2. `git fetch && git checkout <tag-or-sha>`
3. Rebuild: `docker compose build`
4. Rolling restart (workers first or control first — see playbook). Prefer **workers first**, then control, when the new binary is backward-compatible on the wire.
5. Smoke: `./scripts/cluster-smoke.sh`
6. Confirm `/metrics` and registered workers: `curl -sf http://127.0.0.1:18080/v1/workers`

## Compatible upgrades (P1)

Wire compatibility goal within a minor (`0.7.x`):

- Control accepts older workers that still use Bearer `CELL_WORKER_TOKEN`.
- Short-lived tokens remain HMAC over the same secret material.
- Registry JSON on disk is additive; unknown fields ignored where possible.

## Breaking changes

Document here when introducing:

- Renamed env vars / flags
- Auth scheme changes (e.g. mTLS required)
- Registry/meta schema breaks

None since the `0.7.0` line. See [RELEASE.md](RELEASE.md) to cut `v0.7.0`.

## Rollback

1. `git checkout <previous-sha>`
2. `docker compose build && docker compose up -d`
3. If state format moved forward incompatibly, restore `--state-dir` from backup taken before upgrade.
