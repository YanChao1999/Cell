#!/usr/bin/env bash
# Build an ultra-tiny Firecracker rootfs.ext4 with cell-guest-agent as init.
# Contents: static agent + busybox (agent applets) + static rg. Workspace is tmpfs at runtime.
# Output: $CELL_MICROVM_DIR/rootfs.ext4
#
# Knobs:
#   CELL_MICROVM_DIR / CELL_MICROVM_ROOTFS_MB / CELL_BUSYBOX_BIN / CELL_RG_BIN
#   CELL_MICROVM_SKIP_RG=1  — omit ripgrep (saves ~5MiB)
#   CELL_RG_VERSION         — ripgrep release tag (default: latest musl asset)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIR="${CELL_MICROVM_DIR:-${HOME}/.cell/runtime/microvm}"
# Default tiny; override with CELL_MICROVM_ROOTFS_MB, or leave empty for auto-size.
SIZE_MB="${CELL_MICROVM_ROOTFS_MB:-}"
ARCH_GO="${GOARCH:-amd64}"
case "$(uname -m)" in
  aarch64|arm64) ARCH_GO=arm64 ;;
esac

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

mkdir -p "$DIR" \
  "$TMP/root/usr/local/bin" \
  "$TMP/root/bin" \
  "$TMP/root/usr/bin" \
  "$TMP/root/proc" \
  "$TMP/root/sys" \
  "$TMP/root/dev" \
  "$TMP/root/etc"

echo "building cell-guest-agent (linux/${ARCH_GO}, static, stripped)…"
export PATH="${HOME}/.local/go/bin:/usr/local/go/bin:${PATH}"
CGO_ENABLED=0 GOOS=linux GOARCH="${ARCH_GO}" go build -trimpath -ldflags='-s -w' \
  -o "$TMP/root/usr/local/bin/cell-guest-agent" "$ROOT/cmd/cell-guest-agent"
# Extra strip if binutils present (no-op when already stripped).
command -v strip >/dev/null 2>&1 && strip -s "$TMP/root/usr/local/bin/cell-guest-agent" || true

AGENT_BYTES="$(wc -c <"$TMP/root/usr/local/bin/cell-guest-agent" | tr -d ' ')"
echo "  agent: ${AGENT_BYTES} bytes"

# Agent-oriented busybox applets: boot helpers + search/edit/text tooling agents expect.
# (rg is a separate static binary — busybox has no ripgrep.)
BUSYBOX_APPLETS=(
  # RO-root bring-up
  sh ash mount umount mkdir printf echo cat cp ls sleep ln rm mv chmod
  # Agent search / text
  grep find sed awk tr head tail wc sort uniq xargs cut
  # Archives / checksums
  tar gzip gunzip sha256sum
  # Misc shell helpers
  test true false env pwd which clear diff
)

install_busybox() {
  local src="$1"
  install -m 0755 "$src" "$TMP/root/bin/busybox"
  local applet
  for applet in "${BUSYBOX_APPLETS[@]}"; do
    # Skip applets this busybox build does not provide.
    if "$TMP/root/bin/busybox" --list 2>/dev/null | grep -qx "$applet"; then
      ln -sf busybox "$TMP/root/bin/$applet"
    fi
  done
  # '[' is a BRE metachar; match fixed-string.
  if "$TMP/root/bin/busybox" --list 2>/dev/null | grep -Fqx '['; then
    ln -sf busybox "$TMP/root/bin/["
  fi
}

if [[ -n "${CELL_BUSYBOX_BIN:-}" && -x "${CELL_BUSYBOX_BIN}" ]]; then
  install_busybox "${CELL_BUSYBOX_BIN}"
elif command -v busybox >/dev/null 2>&1; then
  install_busybox "$(command -v busybox)"
else
  echo "NOTE: no busybox on PATH — rootfs is agent-only (mount via agent may be limited)" >&2
fi

install_rg() {
  local dest="$TMP/root/usr/bin/rg"
  if [[ -n "${CELL_RG_BIN:-}" && -x "${CELL_RG_BIN}" ]]; then
    install -m 0755 "${CELL_RG_BIN}" "$dest"
    command -v strip >/dev/null 2>&1 && strip -s "$dest" 2>/dev/null || true
    echo "  rg: $(wc -c <"$dest" | tr -d ' ') bytes (CELL_RG_BIN)"
    return 0
  fi
  if [[ "${CELL_MICROVM_SKIP_RG:-}" == "1" ]]; then
    echo "  rg: skipped (CELL_MICROVM_SKIP_RG=1)"
    return 0
  fi

  local triple asset ver url
  case "${ARCH_GO}" in
    amd64) triple="x86_64-unknown-linux-musl" ;;
    arm64) triple="aarch64-unknown-linux-musl" ;;
    *)
      echo "NOTE: no musl rg asset for GOARCH=${ARCH_GO}; set CELL_RG_BIN or CELL_MICROVM_SKIP_RG=1" >&2
      return 0
      ;;
  esac

  ver="${CELL_RG_VERSION:-}"
  if [[ -z "$ver" ]]; then
    ver="$(curl -fsSL https://api.github.com/repos/BurntSushi/ripgrep/releases/latest \
      | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)"
  fi
  if [[ -z "$ver" ]]; then
    echo "NOTE: could not resolve ripgrep release; set CELL_RG_BIN or CELL_RG_VERSION" >&2
    return 0
  fi
  # Assets look like: ripgrep-14.1.1-x86_64-unknown-linux-musl.tar.gz
  asset="ripgrep-${ver#v}-${triple}.tar.gz"
  url="https://github.com/BurntSushi/ripgrep/releases/download/${ver}/${asset}"
  echo "  fetching ${asset}…"
  mkdir -p "$TMP/rg"
  if ! curl -fsSL "$url" -o "$TMP/rg/rg.tgz"; then
    echo "NOTE: rg download failed ($url); set CELL_RG_BIN or CELL_MICROVM_SKIP_RG=1" >&2
    return 0
  fi
  tar -xzf "$TMP/rg/rg.tgz" -C "$TMP/rg"
  local bin
  bin="$(find "$TMP/rg" -type f -name rg | head -1)"
  if [[ -z "$bin" || ! -x "$bin" ]]; then
    echo "NOTE: rg binary missing from archive" >&2
    return 0
  fi
  install -m 0755 "$bin" "$dest"
  command -v strip >/dev/null 2>&1 && strip -s "$dest" 2>/dev/null || true
  echo "  rg: $(wc -c <"$dest" | tr -d ' ') bytes (${ver})"
}

install_rg

printf 'cell\n' >"$TMP/root/etc/hostname"
# Placeholder; allowlist path overwrites via cell.nameserver= / agent.
printf 'nameserver 1.1.1.1\n' >"$TMP/root/etc/resolv.conf"

# Auto-size: content + 40% headroom, min 8MiB, max 64MiB unless overridden.
CONTENT_KB="$(du -sk "$TMP/root" | awk '{print $1}')"
AUTO_MB=$(( (CONTENT_KB + 1023) / 1024 ))
AUTO_MB=$(( AUTO_MB + AUTO_MB / 2 + 2 ))
if (( AUTO_MB < 8 )); then AUTO_MB=8; fi
if (( AUTO_MB > 64 )); then AUTO_MB=64; fi
if [[ -z "${SIZE_MB}" ]]; then
  SIZE_MB="${AUTO_MB}"
fi
echo "packing ext4 (${SIZE_MB}M, content≈${CONTENT_KB}KiB) → $DIR/rootfs.ext4"

IMG="$DIR/rootfs.ext4"
rm -f "$IMG"

# Prefer no-journal ext4; populate without loop via mke2fs -d (userspace).
MKFS_BASE=(mkfs.ext4 -F -L cellroot -O ^has_journal,^resize_inode -m 0 -q)
if ! command -v mkfs.ext4 >/dev/null 2>&1; then
  MKFS_BASE=(mkfs.ext2 -F -L cellroot -m 0 -q)
fi

pack_ok=0
# 1) mke2fs/mkfs.ext4 -d <dir> — no root/loop needed (e2fsprogs ≥1.43).
if "${MKFS_BASE[@]}" -d "$TMP/root" "$IMG" "${SIZE_MB}M" 2>/dev/null; then
  pack_ok=1
elif command -v genext2fs >/dev/null 2>&1; then
  # 2) genext2fs — fully userspace; -b is 1KiB blocks.
  genext2fs -b $((SIZE_MB * 1024)) -d "$TMP/root" "$IMG"
  pack_ok=1
else
  # 3) truncate + mkfs + loop mount (needs privileges).
  truncate -s "${SIZE_MB}M" "$IMG"
  "${MKFS_BASE[@]}" "$IMG"
  mkdir -p "$TMP/mnt"
  if mount -o loop "$IMG" "$TMP/mnt" 2>/dev/null; then
    cp -a "$TMP/root/." "$TMP/mnt/"
    umount "$TMP/mnt"
    pack_ok=1
  fi
fi
if [[ "$pack_ok" -ne 1 ]]; then
  echo "error: need mkfs.ext4 -d, genext2fs, or loop-mount privileges to build rootfs" >&2
  exit 1
fi

# Shrink to minimum packed size when tools allow.
if command -v resize2fs >/dev/null 2>&1 && command -v e2fsck >/dev/null 2>&1; then
  e2fsck -fy "$IMG" >/dev/null 2>&1 || true
  resize2fs -M "$IMG" >/dev/null 2>&1 || true
fi

FINAL_BYTES="$(wc -c <"$IMG" | tr -d ' ')"
FINAL_MB="$(awk -v b="$FINAL_BYTES" 'BEGIN{printf "%.1f", b/1024/1024}')"
echo "rootfs ready: $IMG (${FINAL_BYTES} bytes ≈ ${FINAL_MB} MiB)"
echo "  init=/usr/local/bin/cell-guest-agent"
echo "  export CELL_MICROVM_ROOTFS=$IMG"
if (( FINAL_BYTES > 32 * 1024 * 1024 )); then
  echo "WARN: rootfs > 32MiB — unexpected for ultra-tiny profile" >&2
fi
