# Publish `dsh-plugin-cell` (npm) and `cell-mcp` (PyPI)

Workflow: [`.github/workflows/publish-packages.yml`](../../.github/workflows/publish-packages.yml)

- **Triggers:** GitHub Release `published`, or manual `workflow_dispatch` (default **dry-run**).
- **Does not run on pull requests** (avoids token exfil via fork PRs).
- **Permissions:** `contents: read` + `id-token: write` (OIDC). No `packages: write` beyond registries.

## What you prepare

### 1. npm — `dsh-plugin-cell`

**Option A (recommended while setting up): classic token**

1. npmjs.com → Access Tokens → **Granular** token with **publish** only for package `dsh-plugin-cell` (create the empty package once if needed).
2. GitHub repo → Settings → Secrets → Actions → `NPM_TOKEN`.
3. First publish claims the name: ensure you own `dsh-plugin-cell` on npm.

**Option B: npm Trusted Publishing (OIDC)**  
Configure the GitHub Actions workflow as a trusted publisher on npm for this repo + `publish-packages.yml`, then you can drop `NPM_TOKEN` once npm accepts provenance-only auth for your account.

Package lives in `integrations/dsh-cell/` (`package.json` name/version). Bump version there before cutting a release that should publish a new npm version.

### 2. PyPI — `cell-mcp`

**Preferred: Trusted Publishing (OIDC, no API token in GitHub)**

1. https://pypi.org/manage/account/publishing/ → Add a new pending publisher:
   - **PyPI project name:** `cell-mcp`
   - **Owner:** `YanChao1999` (or your org)
   - **Repository:** `Cell`
   - **Workflow name:** `publish-packages.yml`
   - **Environment:** leave empty unless you add a GitHub Environment
2. First successful OIDC upload creates the project (or claim an existing one you own).

**Fallback:** create a PyPI API token scoped to `cell-mcp`, store as `PYPI_API_TOKEN`, and add `password: ${{ secrets.PYPI_API_TOKEN }}` to the PyPI publish step in the workflow.

Package lives in `integrations/mcp/` (`pyproject.toml` name/version). Bump before release.

### 3. Optional later — `cell-sdk`

Python SDK is `sdk/python` (`cell-sdk` on PyPI). Not in this workflow yet; add a third job the same way when you want it.

## Dry-run before real publish

Actions → **Publish packages** → Run workflow:

| Input | Value |
|-------|--------|
| package | `both` / `npm` / `pypi` |
| dry_run | `true` (default) |

This packs/builds and runs content gates; it does **not** upload. Set `dry_run=false` only after secrets/OIDC are ready.

## Release cut flow

1. Bump adapter versions if needed (`integrations/dsh-cell/package.json`, `integrations/mcp/pyproject.toml`).
2. `./scripts/release.sh x.y.z` (or publish a GitHub Release for an existing tag).
3. `release: published` starts this workflow and uploads when credentials work.
4. Attach checksums of published artifacts in release notes when practical.

## Content gates (already in CI)

- npm: refuse non-empty `package.json` `scripts`; refuse pack paths that look like secrets.
- PyPI: refuse sdist/wheel members that look like secrets; `compileall` on MCP sources.

## Security notes (from 0.8 review)

- Prefer **OIDC** over long-lived publish tokens; scope any token to one package.
- Keep `package.json` `files` / hatch `packages` minimal.
- TCP Agent API requires Bearer (`CELL_WORKER_TOKEN` / short-lived `agent` scope). Unix socket without a token remains local/`0600` trust.
- Verify release downloads with [release-assets-v0.8.0.sha256](release-assets-v0.8.0.sha256).
