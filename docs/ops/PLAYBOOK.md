# Ops playbook — restart & rolling

Assumes the default compose cluster (`docker-compose.yml`, host networking, ports `18080` control / `18081`–`18082` workers).

## Health checks

```bash
curl -sf http://127.0.0.1:18080/health
curl -sf http://127.0.0.1:18080/v1/workers
curl -sf http://127.0.0.1:18080/metrics | head
```

Workers without a token must get `401` on RPC; with token, `ok`:

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:18081/worker/v1/health   # expect 401
curl -sf -H "Authorization: Bearer ${CELL_WORKER_TOKEN:-cell-dev-token}" \
  http://127.0.0.1:18081/worker/v1/health
```

## Full stop / start

```bash
docker compose down
# free host ports if a leftover host celld is bound
docker compose up --build -d
./scripts/cluster-smoke.sh
```

## Rolling restart (zero total outage goal: soft)

Workers re-register via heartbeat after restart. In-flight Cell execs on a restarted worker fail; clients should retry create/exec.

**Preferred order (binary-compatible upgrade):**

1. Rebuild images: `docker compose build`
2. Restart workers one at a time:
   ```bash
   docker compose up -d --no-deps --build worker-a
   # wait until worker-a appears in /v1/workers
   docker compose up -d --no-deps --build worker-b
   ```
3. Restart control last:
   ```bash
   docker compose up -d --no-deps --build control
   ```
4. Run `./scripts/cluster-smoke.sh`

**Control-first** only when the new control must reject old workers (breaking auth/API). Drain traffic first; expect a short registration gap.

## Single-process (`mode=all`)

```bash
pkill -TERM celld || true
# wait for socket release
./bin/celld serve --mode=all --cell-home ~/.cell --environments ./environments
```

State under `--cell-home` / `--state-dir` survives process restart.

## Observability stack

```bash
docker compose -f docker-compose.yml -f docker-compose.obs.yml up -d
# Grafana  http://127.0.0.1:13000  (admin / cell)
# Prometheus http://127.0.0.1:19090
# Built-in UI http://127.0.0.1:18080/metrics/ui
```

Images default to GHCR mirrors (`ghcr.io/yanchao1999/cell-obs-*`). Refresh mirrors via workflow **Mirror obs images**.

## Incident: empty worker list

1. `docker compose logs control worker-a worker-b`
2. Confirm nothing else owns `:18080` (`ss -ltnp | grep 18080`)
3. Confirm `CELL_WORKER_TOKEN` matches on control and workers
4. Recreate workers: `docker compose up -d --force-recreate worker-a worker-b`
