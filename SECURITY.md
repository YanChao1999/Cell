# Security Policy

Cell is an execution boundary for untrusted agent work. Treat reported
isolation bugs as high priority.

## Supported versions

| Version | Supported |
|---------|-----------|
| `0.8.x` (latest release + `main`) | Yes |
| Older tags | Best-effort only — please upgrade |

See [docs/ops/SUPPORT.md](docs/ops/SUPPORT.md) and [docs/ops/UPGRADE.md](docs/ops/UPGRADE.md).

## Reporting a vulnerability

**Do not** open a public GitHub issue for security problems.

Preferred: GitHub Security Advisories (private):

→ [Report a vulnerability](https://github.com/YanChao1999/Cell/security/advisories/new)

Include, when possible:

- Cell version / commit (`celld -version` or module version)
- Runtime (`Docker` / `microvm` / `host`) and host OS
- Topology (single-node `mode=all` vs compose workers)
- Steps to reproduce and expected impact (escape, data leak, auth bypass, DoS)
- Proof-of-concept only as far as needed to demonstrate — no mass exploitation

You should receive an acknowledgment within a few days. We will coordinate a
fix or public advisory when appropriate.

## Scope (examples)

In scope:

- Sandbox escape (Docker or Firecracker/jailer path)
- Auth / token bypass on Agent or worker APIs
- Unsafe snapshot restore / path traversal / tar bombs
- Supply-chain issues in release artifacts (checksum mismatches)

Out of scope (file as normal bugs / features unless they enable escape):

- Misconfiguration (empty tokens, `CELL_MICROVM_ALLOW_UNJAILED` in prod)
- Issues only on `CELL_RUNTIME=host` (dev/CI, not an isolation boundary)
- Denial of service via exhausting operator-chosen cell limits alone

Background: [docs/security/THREAT_MODEL.md](docs/security/THREAT_MODEL.md),
[docs/security/ESCAPE_CHECKLIST.md](docs/security/ESCAPE_CHECKLIST.md).

## Safe harbor

We will not pursue legal action against researchers who:

- Make a good-faith effort to avoid privacy violations and service disruption
- Report promptly through the private advisory channel
- Do not exploit the issue beyond what is needed to demonstrate it
