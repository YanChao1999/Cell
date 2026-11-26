# Cell local ops. Namespace prefix works with multi-goal make:
#   make celld up
#   make celld local-cluster
#   make celld obs
#   make celld down
#   make celld status

SHELL := /bin/bash
COMPOSE := docker compose -f docker-compose.yml -f docker-compose.prebuilt.yml
COMPOSE_OBS := $(COMPOSE) -f docker-compose.obs.yml
MCP_VENV ?= $(HOME)/.cell/mcp-venv

.PHONY: celld up local-cluster obs down status build test microvm microvm-smoke mcp-install help

# Prefix so `make celld up` is valid (make runs both targets).
celld:
	@true

help:
	@echo 'Cell local cluster'
	@echo '  make celld up              build binaries + start Docker-runtime cluster'
	@echo '  make celld local-cluster   same as up'
	@echo '  make celld obs             start Prometheus + Grafana (needs cluster up)'
	@echo '  make celld status          health + workers'
	@echo '  make celld down            stop cluster (+ obs)'
	@echo '  make celld mcp-install     install cell-mcp into $(MCP_VENV)'
	@echo '  make celld microvm         setup Firecracker + jailer + kernel + rootfs + smoke'
	@echo '  make build                 build celld / cell / cell-egress / cell-guest-agent'
	@echo '  make test                  go test worker + control + version'
	@echo ''
	@echo 'Grafana  http://127.0.0.1:13000  (admin/cell)'
	@echo 'Metrics  http://127.0.0.1:18080/metrics/ui'
	@echo 'MCP      .cursor/mcp.json → http://127.0.0.1:18080'

build:
	@command -v go >/dev/null || { echo 'go not on PATH'; exit 127; }
	GOTOOLCHAIN=auto go build -trimpath -ldflags='-s -w' -o bin/celld ./cmd/celld
	GOTOOLCHAIN=auto go build -trimpath -ldflags='-s -w' -o bin/cell ./cmd/cell
	GOTOOLCHAIN=auto CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o bin/cell-egress ./cmd/cell-egress
	GOTOOLCHAIN=auto CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o bin/cell-guest-agent ./cmd/cell-guest-agent

test:
	@command -v go >/dev/null || { echo 'go not on PATH'; exit 127; }
	GOTOOLCHAIN=auto go test ./internal/worker/... ./internal/control/... ./internal/version/

# One-shot MicroVM artifacts under ~/.cell/runtime/microvm (or CELL_MICROVM_DIR).
microvm: build
	./scripts/setup-microvm.sh
	./scripts/microvm-smoke.sh
	@echo ''
	@echo 'Then:'
	@echo '  export PATH="$${CELL_MICROVM_DIR:-$$HOME/.cell/runtime/microvm}:$$PATH"'
	@echo '  export CELL_RUNTIME=microvm'
	@echo '  export CELL_MICROVM_DIR=$${CELL_MICROVM_DIR:-$$HOME/.cell/runtime/microvm}'
	@echo '  export CELL_MICROVM_JAILER=$$CELL_MICROVM_DIR/jailer'
	@echo '  # Dev only: export CELL_MICROVM_ALLOW_UNJAILED=1'
	@echo '  ./bin/celld serve --mode=all --environments ./environments'

microvm-smoke:
	./scripts/microvm-smoke.sh

up local-cluster: build
	mkdir -p /tmp/cell-cluster/{control,worker-a,worker-b}
	$(COMPOSE) up --build -d
	@echo 'cluster up — Agent API http://127.0.0.1:18080 (Bearer cell-dev-token)'

obs:
	$(COMPOSE_OBS) up -d
	@echo 'Grafana   http://127.0.0.1:13000  (admin / cell)'
	@echo 'Prometheus http://127.0.0.1:19090'
	@echo 'Built-in  http://127.0.0.1:18080/metrics/ui'

down:
	-$(COMPOSE_OBS) down --remove-orphans
	-$(COMPOSE) down --remove-orphans

status:
	@$(COMPOSE) ps
	@curl -sf http://127.0.0.1:18080/health | sed 's/^/health: /' || echo 'health: DOWN'
	@curl -sf -H 'Authorization: Bearer cell-dev-token' http://127.0.0.1:18080/v1/workers \
	  | python3 -c 'import sys,json; w=json.load(sys.stdin); print("workers:", ",".join(sorted(x["id"] for x in w)))' \
	  2>/dev/null || echo 'workers: unreachable'

mcp-install:
	@command -v uv >/dev/null || { echo 'uv required (https://github.com/astral-sh/uv)'; exit 127; }
	uv venv $(MCP_VENV)
	uv pip install --python $(MCP_VENV)/bin/python -e integrations/mcp 'mcp>=1.0,<2'
	@echo 'cell-mcp → $(MCP_VENV)/bin/cell-mcp'
	@echo 'reload Cursor MCP (project .cursor/mcp.json)'
