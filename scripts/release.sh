#!/usr/bin/env bash
# Cut a Cell release tag from a clean main checkout.
# Usage: ./scripts/release.sh 0.7.0
set -euo pipefail

VER="${1:-}"
if [[ ! "$VER" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "usage: $0 <major.minor.patch>   e.g. $0 0.7.0" >&2
  exit 2
fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

branch="$(git rev-parse --abbrev-ref HEAD)"
if [[ "$branch" != "main" ]]; then
  echo "refusing: must be on main (now on $branch)" >&2
  exit 1
fi
if [[ -n "$(git status --porcelain)" ]]; then
  echo "refusing: working tree not clean" >&2
  git status -sb >&2
  exit 1
fi
git fetch origin main
if [[ "$(git rev-parse HEAD)" != "$(git rev-parse origin/main)" ]]; then
  echo "refusing: local main != origin/main; pull/rebase first" >&2
  exit 1
fi
if git rev-parse "v${VER}" >/dev/null 2>&1; then
  echo "refusing: tag v${VER} already exists" >&2
  exit 1
fi

VERSION_FILE=internal/version/version.go
if ! grep -q 'const Version =' "$VERSION_FILE"; then
  echo "missing Version in $VERSION_FILE" >&2
  exit 1
fi

# shellcheck disable=SC2016
sed -i -E "s/const Version = \"[^\"]+\"/const Version = \"${VER}\"/" "$VERSION_FILE"
if ! grep -q "const Version = \"${VER}\"" "$VERSION_FILE"; then
  echo "failed to bump Version" >&2
  exit 1
fi

git add "$VERSION_FILE"
git commit -m "Release v${VER}."
git tag -a "v${VER}" -m "Cell v${VER}"
git push origin main
git push origin "v${VER}"

# Suggest next -dev bump (does not apply it).
IFS=. read -r MA MI PA <<<"$VER"
NEXT_DEV="${MA}.${MI}.$((PA + 1))-dev"
cat <<EOF

Tagged and pushed v${VER}.

Next on main (separate commit):
  sed -i -E 's/const Version = "${VER}"/const Version = "${NEXT_DEV}/' ${VERSION_FILE}
  git add ${VERSION_FILE} && git commit -m "Back to ${NEXT_DEV} on main." && git push

Create a GitHub Release from CHANGELOG.md section [${VER}].
EOF
