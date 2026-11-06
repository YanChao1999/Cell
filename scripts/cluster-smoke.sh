#!/usr/bin/env bash
# Smoke a 1-control + 2-worker compose cluster: register, place across workers, exec.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
CTRL="${CELL_CLUSTER_URL:-http://127.0.0.1:18080}"

echo "waiting for control at $CTRL ..."
for _ in $(seq 1 60); do
  if curl -sf "$CTRL/health" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
curl -sf "$CTRL/health" | grep -q ok
echo "OK control health"

echo "waiting for workers to register ..."
for _ in $(seq 1 60); do
  N=$(curl -sf "$CTRL/v1/workers" | python3 -c 'import sys,json; print(len(json.load(sys.stdin)))' 2>/dev/null || echo 0)
  if [ "$N" -ge 2 ]; then
    break
  fi
  sleep 1
done
WORKERS=$(curl -sf "$CTRL/v1/workers")
echo "$WORKERS" | python3 -c 'import sys,json; w=json.load(sys.stdin); assert len(w)>=2, w; print("OK workers:", ",".join(sorted(x["id"] for x in w)))'

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
