#!/usr/bin/env bash
# Soft MicroVM smoke: verify artifacts; optional Firecracker version check.
# Full guest boot needs /dev/kvm + nested virt — skipped cleanly when absent.
set -euo pipefail

DIR="${CELL_MICROVM_DIR:-${HOME}/.cell/runtime/microvm}"

need() {
  if [[ ! -e "$1" ]]; then
    echo "missing $1 — run ./scripts/setup-microvm.sh first" >&2
    exit 1
  fi
}

need "$DIR/rootfs.ext4"
echo "rootfs ok: $(ls -lh "$DIR/rootfs.ext4")"

if [[ ! -x "$DIR/firecracker" || ! -f "$DIR/vmlinux" ]]; then
  echo "NOTE: firecracker/vmlinux missing — rootfs-only smoke (run ./scripts/setup-microvm.sh for full set)"
  exit 0
fi

echo "artifacts ok under $DIR"
"$DIR/firecracker" --version 2>/dev/null | head -3 || true
ls -lh "$DIR/vmlinux" "$DIR/rootfs.ext4"

if [[ ! -e /dev/kvm ]]; then
  echo "SKIP: /dev/kvm not present — artifact smoke only (no guest boot)"
  exit 0
fi
if [[ ! -r /dev/kvm ]]; then
  echo "SKIP: /dev/kvm not readable — artifact smoke only"
  exit 0
fi

echo "OK: /dev/kvm present — operator can boot with CELL_RUNTIME=microvm"
echo "    (full InstanceStart smoke is left to nested-virt CI / local celld)"
exit 0
