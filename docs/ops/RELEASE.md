# Release checklist (`v0.7.0` and later)

## Preconditions

- [ ] P1 merge-ready items on `main` (ops docs, CVE CI, compose smoke, microVM M1, Dependabot/digests)
- [ ] CI green on `main`
- [ ] `CHANGELOG.md` section for the version filled in

## Cut the tag

```bash
# on main, clean tree
./scripts/release.sh 0.7.0
```

The script:

1. Asserts a clean working tree on `main`
2. Sets `internal/version.Version` to `0.7.0` (drops `-dev`)
3. Commits, tags `v0.7.0`, pushes branch + tag
4. Leaves a follow-up note to bump back to `0.7.1-dev` (or next) on main

## After tag

- [ ] GitHub Release: paste the `CHANGELOG` section; attach `celld`/`cell`/`cell-egress`/`cell-guest-agent` if you build artifacts
- [ ] Compose users: `git checkout v0.7.0 && docker compose build && …` per [UPGRADE.md](UPGRADE.md)
- [ ] Open a tiny PR or commit on main restoring `Version = "0.7.1-dev"` (script prints the exact sed)

## Image promotion (optional)

No registry publish pipeline yet. Operators rebuild from the tag:

```bash
git checkout v0.7.0
docker compose build
docker tag cell-control:latest your.registry/cell-control:0.7.0
```
