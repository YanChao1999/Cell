#!/usr/bin/env bash
# Smoke test: health, create/exec/destroy, one-shot run (host fallback OK without Docker).
# Uses an ephemeral --cell-home by default so it does not touch ~/.cell.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="${HOME}/.local/go/bin:/usr/local/go/bin:${PATH}"
# Default to host execution so smoke does not pull images when Docker is installed.
export CELL_RUNTIME="${CELL_RUNTIME:-host}"

CLEANUP_HOME=0
if [ -z "${CELL_HOME:-}" ]; then
  CELL_HOME="$(mktemp -d "${TMPDIR:-/tmp}/cell-smoke.XXXXXX")"
  CLEANUP_HOME=1
fi
SOCKET="${CELL_SOCKET:-${CELL_HOME}/celld.sock}"
CACHE_DIR="${CELL_CACHE_DIR:-${CELL_HOME}/cache}"
LOG_FILE="${CELL_SMOKE_LOG:-${CELL_HOME}/celld-smoke.log}"
mkdir -p "$CELL_HOME" "$(dirname "$SOCKET")"

cd "$ROOT"
go build -o bin/celld ./cmd/celld
go build -o bin/cell ./cmd/cell
CGO_ENABLED=0 go build -o bin/cell-egress ./cmd/cell-egress

# Prefer killing only our eventual PID; avoid broad pkill in CI.
if [ -S "$SOCKET" ]; then
  rm -f "$SOCKET"
fi
./bin/celld serve --mode=all \
  --cell-home "$CELL_HOME" \
  --socket "$SOCKET" \
  --environments ./environments \
  --cache-dir "$CACHE_DIR" \
  > "$LOG_FILE" 2>&1 &
PID=$!
cleanup() {
  kill $PID 2>/dev/null || true
  wait $PID 2>/dev/null || true
  if [ "$CLEANUP_HOME" = 1 ]; then
    rm -rf "$CELL_HOME"
  fi
}
trap cleanup EXIT
for _ in $(seq 1 50); do
  if curl -sf --unix-socket "$SOCKET" http://localhost/health >/dev/null 2>&1; then
    break
  fi
  sleep 0.1
done

curl -sf --unix-socket "$SOCKET" http://localhost/health | grep -q ok
echo "OK health (cell-home=$CELL_HOME)"

if ! CREATE=$(curl -sf --unix-socket "$SOCKET" -X POST http://localhost/v1/cells \
  -H 'Content-Type: application/json' \
  -d '{"environment":"go-1.23","ttl":"5m"}'); then
  echo "create failed; daemon log:" >&2
  tail -n 80 "$LOG_FILE" >&2 || true
  exit 1
fi
ID=$(printf '%s' "$CREATE" | python3 -c 'import sys,json; print(json.load(sys.stdin)["id"])')
# Create runs template setup (go version); cell should be ready.
STATUS=$(printf '%s' "$CREATE" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("status",""))')
test "$STATUS" = "ready"
EXEC=$(curl -sf --unix-socket "$SOCKET" -X POST "http://localhost/v1/cells/${ID}/exec" \
  -H 'Content-Type: application/json' \
  -d '{"command":"echo smoke-ok"}')
printf '%s' "$EXEC" | grep -q smoke-ok
curl -sf --unix-socket "$SOCKET" -X DELETE "http://localhost/v1/cells/${ID}" -o /dev/null -w ''
echo "OK create/setup/exec/destroy"

RUN=$(curl -sf --unix-socket "$SOCKET" -X POST http://localhost/v1/run \
  -H 'Content-Type: application/json' \
  -d '{"environment":"go-1.23","commands":["echo run-ok"]}')
printf '%s' "$RUN" | grep -q run-ok
echo "OK /v1/run"

export CELL_SOCKET="$SOCKET"
./bin/cell health >/dev/null
./bin/cell run --env go-1.23 'echo cli-run' | grep -q cli-run
echo "OK cell CLI"

# Lightweight package checks only (not a full unit suite).
go test ./internal/worker/cache/ ./internal/worker/envtemplate/ ./internal/worker/egress/ ./internal/worker/manager/ ./internal/worker/source/git/
echo "OK package checks"
echo "smoke passed"
