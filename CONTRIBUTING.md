# Contributing to Cell

Thanks for helping improve Cell. This guide matches how the repo is actually
developed and reviewed on GitHub.

## Before you start

1. Read [README.md](README.md) and [docs/quickstart.md](docs/quickstart.md).
2. Check [docs/ops/SUPPORT.md](docs/ops/SUPPORT.md) so you know what is in / out of scope.
3. For security issues, **do not open a public issue** — follow [SECURITY.md](SECURITY.md).
4. Search existing issues / PRs before filing a new one.

## Development setup

```bash
# Go 1.26+ (GOTOOLCHAIN=auto works)
export PATH=$HOME/.local/go/bin:$PATH
go test ./internal/worker/... ./internal/control/... ./internal/version/

# Local Docker-runtime cluster (optional, for integration)
make celld up
make celld status
make celld obs          # Prometheus :19090 · Grafana :13000
make celld mcp-install  # Cursor MCP → .cursor/mcp.json
```

Useful docs:

- Architecture: [ARCHITECTURE.md](ARCHITECTURE.md)
- Custom images / limits: [docs/custom-images.md](docs/custom-images.md)
- Ops playbooks: [docs/ops/](docs/ops/)

## Pull requests

1. Branch from `main` (or the active release branch named in the issue).
2. Keep PRs focused — one concern per PR when practical.
3. Include tests for behavioral changes under `internal/…`.
4. Update docs / `CHANGELOG.md` when user-facing behavior changes.
5. Fill in the PR template (what / why / how tested).
6. Ensure CI is green (`.github/workflows/ci.yml`).

### Commit style

Prefer short imperative subjects that say **why**:

```
Add async create so agents can poll progress without hanging MCP.
```

### What we usually accept

- Bug fixes with a clear reproduction
- Metrics / observability that help operators tune cold creates
- Env templates, docs, MCP/SDK ergonomics
- Hardening that matches [docs/security/THREAT_MODEL.md](docs/security/THREAT_MODEL.md)

### What needs an issue first

- New runtimes or network modes
- Breaking API / schema changes
- Large refactors without a linked bug or design note

## Issues

Use the GitHub issue forms:

- **Bug report** — expected vs actual, version, runtime (Docker / microVM / host), logs
- **Feature request** — problem, proposed UX, alternatives

Label lightly; maintainers will triage.

## Code of Conduct

Participation is governed by [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

## License

By contributing, you agree that your contributions are licensed under the MIT
License ([LICENSE](LICENSE)).
