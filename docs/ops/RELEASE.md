# Release checklist (`v0.8.0` and later)

## Preconditions

- [ ] Productization items on `main` (quickstart, SUPPORT, setup-microvm, MCP + DSH plugin, microVM M3)
- [ ] CI green on `main`
- [ ] `CHANGELOG.md` section for the version filled in (move Unreleased → dated section)

## Cut the tag

```bash
# on main, clean tree
./scripts/release.sh 0.8.0
```

The script:

1. Asserts a clean working tree on `main`
2. Sets `internal/version.Version` to `0.8.0` (drops `-dev`)
3. Commits, tags `v0.8.0`, pushes branch + tag
4. Leaves a follow-up note to bump back to `0.8.1-dev` (or next) on main

## After tag

- [ ] GitHub Release: paste the `CHANGELOG` section; attach binaries + note CI `cell-microvm-rootfs` artifact
- [ ] Compose users: `git checkout v0.8.0 && docker compose build && …` per [UPGRADE.md](UPGRADE.md)
- [ ] Open a tiny PR or commit on main restoring `Version = "0.8.1-dev"` (script prints the exact sed)
- [ ] npm / PyPI: after [PUBLISH.md](PUBLISH.md) credentials are ready, a Release `published` event runs `publish-packages.yml` (`dsh-plugin-cell`, `cell-mcp`). Dry-run via workflow_dispatch first.

## Image promotion (optional)

No registry publish pipeline yet. Operators rebuild from the tag:

```bash
git checkout v0.8.0
docker compose build
docker tag cell-control:latest your.registry/cell-control:0.8.0
```
