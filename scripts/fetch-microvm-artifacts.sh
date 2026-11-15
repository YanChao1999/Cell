#!/usr/bin/env bash
# Fetch Firecracker + recommended kernel into CELL_MICROVM_DIR (default ~/.cell/runtime/microvm).
# Rootfs must still be built (see docs below) — we only fetch VMM + kernel.
set -euo pipefail

DIR="${CELL_MICROVM_DIR:-${HOME}/.cell/runtime/microvm}"
FC_VERSION="${CELL_FIRECRACKER_VERSION:-v1.11.0}"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH=x86_64 ;;
  aarch64|arm64) ARCH=aarch64 ;;
  *) echo "unsupported arch: $ARCH" >&2; exit 1 ;;
esac

mkdir -p "$DIR"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "fetching firecracker ${FC_VERSION} (${ARCH}) → ${DIR}"
BASE="https://github.com/firecracker-microvm/firecracker/releases/download/${FC_VERSION}"
curl -fsSL "${BASE}/firecracker-${FC_VERSION}-${ARCH}.tgz" -o "$TMP/fc.tgz"
tar -xzf "$TMP/fc.tgz" -C "$TMP"
# Release layout: release-${VERSION}-${ARCH}/firecracker-${VERSION}-${ARCH}
FC_BIN="$(find "$TMP" -type f -name 'firecracker-*' ! -name '*.tgz' ! -name '*jailer*' | head -n1)"
install -m 0755 "$FC_BIN" "$DIR/firecracker"
JAILER_BIN="$(find "$TMP" -type f -name 'jailer-*' ! -name '*.tgz' | head -n1 || true)"
if [[ -n "${JAILER_BIN}" ]]; then
  install -m 0755 "$JAILER_BIN" "$DIR/jailer"
fi

# Official CI kernels are published alongside firecracker releases as vmlinux.
# Prefer an operator-provided kernel; otherwise try the release asset name used by FC docs.
if [[ ! -f "$DIR/vmlinux" ]]; then
  if curl -fsSL "${BASE}/vmlinux-${ARCH}.bin" -o "$DIR/vmlinux" 2>/dev/null; then
    echo "downloaded vmlinux-${ARCH}.bin"
  else
    echo "NOTE: place a Firecracker-compatible vmlinux at ${DIR}/vmlinux" >&2
  fi
fi

cat <<EOF
Artifacts dir: $DIR
  firecracker → $DIR/firecracker
  jailer      → $DIR/jailer     (required in prod; or CELL_MICROVM_ALLOW_UNJAILED=1)
  kernel      → $DIR/vmlinux   (required)
  rootfs      → $DIR/rootfs.ext4 (ultra-tiny; build with scripts/build-microvm-rootfs.sh)

Build rootfs (agent + busybox applets + static rg, auto-sized ~8–32MiB, RO at runtime):
  ./scripts/build-microvm-rootfs.sh
  # optional: CELL_MICROVM_ROOTFS_MB=16 CELL_BUSYBOX_BIN=… CELL_RG_BIN=… CELL_MICROVM_SKIP_RG=1

Env:
  export PATH=\"$DIR:\$PATH\"
  export CELL_RUNTIME=microvm
  export CELL_MICROVM_DIR=$DIR
  export CELL_MICROVM_JAILER=$DIR/jailer
EOF
