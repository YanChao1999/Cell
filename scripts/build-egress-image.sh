#!/usr/bin/env bash
# Build the dedicated cell-egress sidecar image (cell-egress:local by default).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TAG="${CELL_EGRESS_IMAGE:-cell-egress:local}"
cd "$ROOT"
docker build -f docker/cell-egress/Dockerfile --target runtime -t "$TAG" .
echo "built $TAG"
