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

sha256_file() {
  local f="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$f" | awk '{print $1}'
  else
    shasum -a 256 "$f" | awk '{print $1}'
  fi
}

verify_sha256() {
  local f="$1" want="$2" label="$3"
  local got
  got="$(sha256_file "$f")"
  if [[ "$got" != "$want" ]]; then
    echo "ERROR: $label sha256 mismatch (got $got want $want)" >&2
    return 1
  fi
  echo "verified $label ($got)"
}

# Known checksums for default Firecracker release tarballs (entire .tgz).
# Override with CELL_FIRECRACKER_TGZ_SHA256 if using a different version.
known_fc_sha() {
  case "${FC_VERSION}:${ARCH}" in
    v1.11.0:x86_64) echo "38ad6fb34273b2fa616956237b15ea6e064cf21336b0d990d5de347b35b9328b" ;;
    v1.11.0:aarch64) echo "4b98f7cd669a772716fd1bef59c75188ba05a683bc0759ee4169eb351274fcb0" ;;
    *) echo "" ;;
  esac
}

echo "fetching firecracker ${FC_VERSION} (${ARCH}) → ${DIR}"
BASE="https://github.com/firecracker-microvm/firecracker/releases/download/${FC_VERSION}"
curl -fsSL "${BASE}/firecracker-${FC_VERSION}-${ARCH}.tgz" -o "$TMP/fc.tgz"

FC_SHA="${CELL_FIRECRACKER_TGZ_SHA256:-}"
if [[ -z "$FC_SHA" ]]; then
  # Prefer upstream .sha256.txt when present; else known pin for default version.
  if curl -fsSL "${BASE}/firecracker-${FC_VERSION}-${ARCH}.tgz.sha256.txt" -o "$TMP/fc.sha256" 2>/dev/null; then
    FC_SHA="$(awk '{print $1; exit}' "$TMP/fc.sha256")"
  else
    FC_SHA="$(known_fc_sha)"
  fi
fi
if [[ -z "$FC_SHA" ]]; then
  echo "ERROR: no checksum for firecracker ${FC_VERSION}/${ARCH}; set CELL_FIRECRACKER_TGZ_SHA256" >&2
  exit 1
fi
verify_sha256 "$TMP/fc.tgz" "$FC_SHA" "firecracker tgz"

tar -xzf "$TMP/fc.tgz" -C "$TMP"
# Release layout: release-${VERSION}-${ARCH}/firecracker-${VERSION}-${ARCH}
FC_BIN="$(find "$TMP" -type f -name 'firecracker-*' ! -name '*.tgz' ! -name '*jailer*' | head -n1)"
install -m 0755 "$FC_BIN" "$DIR/firecracker"
JAILER_BIN="$(find "$TMP" -type f -name 'jailer-*' ! -name '*.tgz' | head -n1 || true)"
if [[ -n "${JAILER_BIN}" ]]; then
  install -m 0755 "$JAILER_BIN" "$DIR/jailer"
fi

# Prefer existing kernel; else FC release asset; else Amazon public quickstart kernel.
# Quickstart S3 kernel requires CELL_MICROVM_VMLINUX_SHA256 or explicit unverified allow.
if [[ ! -f "$DIR/vmlinux" ]]; then
  if curl -fsSL "${BASE}/vmlinux-${ARCH}.bin" -o "$DIR/vmlinux" 2>/dev/null; then
    echo "downloaded vmlinux-${ARCH}.bin"
    if [[ -n "${CELL_MICROVM_VMLINUX_SHA256:-}" ]]; then
      verify_sha256 "$DIR/vmlinux" "$CELL_MICROVM_VMLINUX_SHA256" "vmlinux"
    else
      echo "NOTE: set CELL_MICROVM_VMLINUX_SHA256 to pin kernel integrity" >&2
    fi
  elif [[ -n "${CELL_MICROVM_VMLINUX_SHA256:-}" ]] || [[ "${CELL_MICROVM_ALLOW_UNVERIFIED_KERNEL:-}" == "1" ]]; then
    curl -fsSL "https://s3.amazonaws.com/spec.ccfc.min/img/quickstart_guide/${ARCH}/kernels/vmlinux.bin" -o "$DIR/vmlinux"
    echo "downloaded Firecracker quickstart vmlinux.bin (${ARCH})"
    if [[ -n "${CELL_MICROVM_VMLINUX_SHA256:-}" ]]; then
      verify_sha256 "$DIR/vmlinux" "$CELL_MICROVM_VMLINUX_SHA256" "vmlinux"
    else
      echo "WARNING: quickstart kernel installed without checksum (CELL_MICROVM_ALLOW_UNVERIFIED_KERNEL=1)" >&2
    fi
  else
    echo "NOTE: place a Firecracker-compatible vmlinux at ${DIR}/vmlinux" >&2
    echo "  or set CELL_MICROVM_VMLINUX_SHA256 / CELL_MICROVM_ALLOW_UNVERIFIED_KERNEL=1 for S3 quickstart fallback" >&2
  fi
fi

cat <<EOF
Artifacts dir: $DIR
  firecracker → $DIR/firecracker
  jailer      → $DIR/jailer     (required in prod; or CELL_MICROVM_ALLOW_UNJAILED=1)
  kernel      → $DIR/vmlinux   (required)
  rootfs      → $DIR/rootfs.ext4 (ultra-tiny; build with scripts/build-microvm-rootfs.sh)

One-shot (preferred): ./scripts/setup-microvm.sh
Or build rootfs only:
  ./scripts/build-microvm-rootfs.sh
  # optional: CELL_MICROVM_ROOTFS_MB=16 CELL_BUSYBOX_BIN=… CELL_RG_BIN=… CELL_MICROVM_SKIP_RG=1
  # optional: CELL_RG_SHA256=… when downloading rg

Env:
  export PATH=\"$DIR:\$PATH\"
  export CELL_RUNTIME=microvm
  export CELL_MICROVM_DIR=$DIR
  export CELL_MICROVM_JAILER=$DIR/jailer
EOF
