#!/usr/bin/env bash
# Smoke a 1-control + 2-worker compose cluster: register, place across workers, exec.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
CTRL="${CELL_CLUSTER_URL:-http://127.0.0.1:18080}"

echo "waiting for control at $CTRL ..."
READY=0
for _ in $(seq 1 60); do
  if curl -sf "$CTRL/health" >/dev/null 2>&1; then
    READY=1
    break
  fi
  sleep 1
done
if [ "$READY" != 1 ]; then
  echo "control not reachable at $CTRL after 60s." >&2
  echo "Start the cluster first:" >&2
  echo "  docker compose up --build -d" >&2
  echo "Or point CELL_CLUSTER_URL at a running control plane." >&2
  exit 1
fi
curl -sf "$CTRL/health" | grep -q ok
echo "OK control health"

echo "waiting for workers to register ..."
WORKERS='[]'
for _ in $(seq 1 60); do
  WORKERS=$(curl -sf "$CTRL/v1/workers" 2>/dev/null || echo '[]')
  N=$(printf '%s' "$WORKERS" | python3 -c 'import sys,json; print(len(json.load(sys.stdin)))' 2>/dev/null || echo 0)
  if [ "$N" -ge 2 ]; then
    break
  fi
  sleep 1
done
N=$(printf '%s' "$WORKERS" | python3 -c 'import sys,json; print(len(json.load(sys.stdin)))' 2>/dev/null || echo 0)
if [ "$N" -lt 2 ]; then
  echo "expected >=2 registered workers, got: $WORKERS" >&2
  echo "If using compose: docker compose logs worker-a worker-b" >&2
  echo "Also check nothing else owns $CTRL (e.g. leftover ./bin/celld on :18080)." >&2
  exit 1
fi
printf '%s' "$WORKERS" | python3 -c 'import sys,json; w=json.load(sys.stdin); print("OK workers:", ",".join(sorted(x["id"] for x in w)))'

# Worker RPC must reject unauthenticated calls when CELL_WORKER_TOKEN is set in compose.
CODE=$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:18081/worker/v1/health || true)
if [ "$CODE" != "401" ]; then
  echo "expected worker RPC 401 without token, got $CODE" >&2
  exit 1
fi
TOKEN="${CELL_WORKER_TOKEN:-cell-dev-token}"
curl -sf -H "Authorization: Bearer $TOKEN" http://127.0.0.1:18081/worker/v1/health | grep -q ok
echo "OK worker RPC auth"

create_one() {
  curl -sf "$CTRL/v1/cells" -H 'Content-Type: application/json' -d '{
    "environment":"go-1.23",
    "ttl":"5m",
    "prefer_local": false
  }'
}

A=$(create_one)
B=$(create_one)
IDA=$(printf '%s' "$A" | python3 -c 'import sys,json; print(json.load(sys.stdin)["id"])')
IDB=$(printf '%s' "$B" | python3 -c 'import sys,json; print(json.load(sys.stdin)["id"])')
WA=$(printf '%s' "$A" | python3 -c 'import sys,json; print(json.load(sys.stdin)["worker_id"])')
WB=$(printf '%s' "$B" | python3 -c 'import sys,json; print(json.load(sys.stdin)["worker_id"])')
echo "created $IDA on $WA"
echo "created $IDB on $WB"
if [ "$WA" = "$WB" ]; then
  echo "WARN: both Cells on same worker ($WA); capacity may be serial — continuing" >&2
else
  echo "OK placed on different workers"
fi

EXEC=$(curl -sf -X POST "$CTRL/v1/cells/${IDA}/exec" -H 'Content-Type: application/json' -d '{"command":"echo cluster-ok"}')
printf '%s' "$EXEC" | grep -q cluster-ok
echo "OK exec on $WA"

curl -sf -X DELETE "$CTRL/v1/cells/${IDA}" -o /dev/null
curl -sf -X DELETE "$CTRL/v1/cells/${IDB}" -o /dev/null
echo "OK destroy"
echo "cluster smoke passed"
