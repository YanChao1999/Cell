#!/usr/bin/env bash
# Smoke test: health, create/exec/destroy, one-shot run (host fallback OK without Docker).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="${HOME}/.local/go/bin:/usr/local/go/bin:${PATH}"
SOCKET="${CELL_SOCKET:-${HOME}/.cell/celld.sock}"
CELL_HOME="${CELL_HOME:-${HOME}/.cell}"
CACHE_DIR="${CELL_CACHE_DIR:-${CELL_HOME}/cache}"
LOG_FILE="${CELL_SMOKE_LOG:-/tmp/celld-smoke.log}"
mkdir -p "$CELL_HOME" "$(dirname "$SOCKET")"

cd "$ROOT"
go build -o bin/celld ./cmd/celld
go build -o bin/cell ./cmd/cell

# Prefer killing only our eventual PID; avoid broad pkill in CI.
if [ -S "$SOCKET" ]; then
  rm -f "$SOCKET"
fi
./bin/celld serve --mode=all \
  --socket "$SOCKET" \
  --environments ./environments \
  --cache-dir "$CACHE_DIR" \
  > "$LOG_FILE" 2>&1 &
PID=$!
trap 'kill $PID 2>/dev/null || true; wait $PID 2>/dev/null || true' EXIT
for _ in $(seq 1 50); do
  if curl -sf --unix-socket "$SOCKET" http://localhost/health >/dev/null 2>&1; then
    break
  fi
  sleep 0.1
done

curl -sf --unix-socket "$SOCKET" http://localhost/health | grep -q ok
echo "OK health"

CREATE=$(curl -sf --unix-socket "$SOCKET" -X POST http://localhost/v1/cells \
  -H 'Content-Type: application/json' \
  -d '{"environment":"go-1.23","ttl":"5m"}')
ID=$(printf '%s' "$CREATE" | python3 -c 'import sys,json; print(json.load(sys.stdin)["id"])')
EXEC=$(curl -sf --unix-socket "$SOCKET" -X POST "http://localhost/v1/cells/${ID}/exec" \
  -H 'Content-Type: application/json' \
  -d '{"command":"echo smoke-ok"}')
printf '%s' "$EXEC" | grep -q smoke-ok
curl -sf --unix-socket "$SOCKET" -X DELETE "http://localhost/v1/cells/${ID}" -o /dev/null -w ''
echo "OK create/exec/destroy"

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
go test ./internal/worker/cache/ ./internal/worker/envtemplate/
echo "OK package checks"
echo "smoke passed"
