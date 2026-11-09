#!/usr/bin/env bash
# Local 1-control + 2-worker smoke without Docker (host runtime).
# Avoids bash `A && B &` pitfalls that background the CLUSTER_HOME assignment.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# Non-login shells (and some IDE terminals) often miss Go's install dir.
for d in "$HOME/.local/go/bin" /usr/local/go/bin "$HOME/go/bin"; do
  if [[ -x "$d/go" ]]; then
    case ":$PATH:" in
      *":$d:"*) ;;
      *) PATH="$d:$PATH" ;;
    esac
  fi
done
export PATH
if ! command -v go >/dev/null 2>&1; then
  echo "error: go not found (tried PATH, ~/.local/go/bin, /usr/local/go/bin, ~/go/bin)" >&2
  exit 127
fi

GOTOOLCHAIN="${GOTOOLCHAIN:-go1.26.10}"
go build -o bin/celld ./cmd/celld

CLUSTER_HOME="$(mktemp -d /tmp/cell-cluster.XXXXXX)"
echo "CLUSTER_HOME=$CLUSTER_HOME"

cleanup() {
  if [[ -n "${CPID:-}" ]]; then kill "$CPID" 2>/dev/null || true; fi
  if [[ -n "${APID:-}" ]]; then kill "$APID" 2>/dev/null || true; fi
  if [[ -n "${BPID:-}" ]]; then kill "$BPID" 2>/dev/null || true; fi
  wait 2>/dev/null || true
  rm -rf "$CLUSTER_HOME"
}
trap cleanup EXIT

./bin/celld serve \
  --mode=control \
  --listen-http=127.0.0.1:18080 \
  --cell-home="$CLUSTER_HOME/control" \
  --worker-token=cell-dev-token \
  >"$CLUSTER_HOME/control.log" 2>&1 &
CPID=$!

CELL_RUNTIME=host CELL_ADVERTISE_HOST=127.0.0.1 ./bin/celld serve \
  --mode=worker \
  --worker-id=worker-a \
  --listen-http=127.0.0.1:18081 \
  --control-url=http://127.0.0.1:18080 \
  --cell-home="$CLUSTER_HOME/a" \
  --environments="$ROOT/environments" \
  --worker-token=cell-dev-token \
  >"$CLUSTER_HOME/a.log" 2>&1 &
APID=$!

CELL_RUNTIME=host CELL_ADVERTISE_HOST=127.0.0.1 ./bin/celld serve \
  --mode=worker \
  --worker-id=worker-b \
  --listen-http=127.0.0.1:18082 \
  --control-url=http://127.0.0.1:18080 \
  --cell-home="$CLUSTER_HOME/b" \
  --environments="$ROOT/environments" \
  --worker-token=cell-dev-token \
  >"$CLUSTER_HOME/b.log" 2>&1 &
BPID=$!

sleep 1
set +e
CELL_WORKER_TOKEN=cell-dev-token ./scripts/cluster-smoke.sh
EC=$?
set -e
echo "CLUSTER:$EC"
echo '--- control.log ---'
tail -30 "$CLUSTER_HOME/control.log" || true
echo '--- a.log ---'
tail -20 "$CLUSTER_HOME/a.log" || true
echo '--- b.log ---'
tail -20 "$CLUSTER_HOME/b.log" || true
exit "$EC"
