#!/usr/bin/env bash
# One-shot MicroVM artifact setup: Firecracker + jailer + kernel + ultra-tiny rootfs.
# Usage: ./scripts/setup-microvm.sh
# Optional: CELL_MICROVM_DIR=… CELL_MICROVM_SKIP_RG=1 CELL_RG_VERSION=15.2.0
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIR="${CELL_MICROVM_DIR:-${HOME}/.cell/runtime/microvm}"
export CELL_MICROVM_DIR="$DIR"

chmod +x "$ROOT/scripts/fetch-microvm-artifacts.sh" "$ROOT/scripts/build-microvm-rootfs.sh"

echo "==> fetch Firecracker / jailer / kernel"
"$ROOT/scripts/fetch-microvm-artifacts.sh"

echo "==> build ultra-tiny rootfs"
"$ROOT/scripts/build-microvm-rootfs.sh"

missing=0
for f in firecracker vmlinux rootfs.ext4; do
  if [[ ! -f "$DIR/$f" ]]; then
    echo "ERROR: missing $DIR/$f" >&2
    missing=1
  fi
done
if [[ "$missing" -ne 0 ]]; then
  exit 1
fi

BYTES="$(wc -c <"$DIR/rootfs.ext4" | tr -d ' ')"
cat <<EOF

MicroVM ready under $DIR
  firecracker  $($DIR/firecracker --version 2>/dev/null | head -1 || echo present)
  jailer       $([[ -x $DIR/jailer ]] && echo present || echo MISSING — set CELL_MICROVM_ALLOW_UNJAILED=1 for dev)
  vmlinux      $(wc -c <"$DIR/vmlinux" | tr -d ' ') bytes
  rootfs.ext4  ${BYTES} bytes

export PATH="$DIR:\$PATH"
export CELL_RUNTIME=microvm
export CELL_MICROVM_DIR=$DIR
export CELL_MICROVM_JAILER=$DIR/jailer
# Dev without jailer privileges:
# export CELL_MICROVM_ALLOW_UNJAILED=1

Then: go build -o bin/celld ./cmd/celld && ./bin/celld serve --mode=all …
Smoke (needs /dev/kvm): ./scripts/microvm-smoke.sh
EOF
