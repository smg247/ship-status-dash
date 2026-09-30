.PHONY: build e2e test mcp-venv mcp-test mcp-test-api mcp-test-dev local-dashboard-dev local-component-monitor-dev lint npm build-dashboard build-frontend build-component-monitor component-monitor-dry-run apm verify-apm bdd

build: build-frontend build-dashboard

local-e2e:
	@./test/e2e/scripts/local-e2e.sh

test: bdd
	@echo "Running unit tests..."
	@gotestsum -- ./pkg/... ./cmd/... -v

mcp-venv:
	@for d in mcp ship-status-dev; do \
		if [ ! -x $$d/.venv/bin/pytest ]; then \
			echo "Creating $$d/.venv..."; \
			UV_CACHE_DIR=/tmp/uv-cache uv venv --python '>=3.10' $$d/.venv && UV_CACHE_DIR=/tmp/uv-cache uv pip install --python $$d/.venv/bin/python -q -r $$d/requirements-dev.txt; \
		fi; \
	done

mcp-test: mcp-venv mcp-test-api mcp-test-dev

mcp-test-api:
	@mcp/.venv/bin/pytest mcp/ -q

mcp-test-dev:
	@ship-status-dev/.venv/bin/pytest ship-status-dev/ -q

lint: npm
	@./hack/go-lint.sh --timeout 10m run ./...
	@cd frontend && npm run lint
	@cd frontend && npm audit --omit=dev

npm:
	@cd frontend && npm ci --no-audit --ignore-scripts --cache /tmp/npm-cache

build-dashboard:
	@go build -mod=vendor -o dashboard ./cmd/dashboard

build-frontend: npm
	@cd frontend && \
	VITE_PUBLIC_DOMAIN=https://ship-status.ci.openshift.org \
	VITE_PROTECTED_DOMAIN=https://protected.ship-status.ci.openshift.org \
	npm run build

build-component-monitor:
	@go build -mod=vendor -o component-monitor ./cmd/component-monitor

component-monitor-dry-run:
	@./hack/component-monitor-dry-run/create-job.sh

bdd: npm
	@if [ -n "$$CI" ]; then cd frontend && npm run test:e2e; else cd frontend && env -u PLAYWRIGHT_BROWSERS_PATH npm run test:e2e; fi

_uvx_env = $(if $(filter true,$(CI)),UV_CACHE_DIR=/tmp/uv-cache UV_TOOL_DIR=/tmp/uv-tools)
apm:
	@command -v uvx >/dev/null || (echo "uvx not found; install uv (see .devcontainer/Dockerfile)" >&2 && exit 1)
	$(_uvx_env) uvx --from apm-cli@0.13.0 apm install
	$(_uvx_env) uvx --from apm-cli@0.13.0 apm compile

APM_GENERATED_FILES := .claude .cursor .gemini .opencode AGENTS.md CLAUDE.md GEMINI.md frontend/AGENTS.md frontend/CLAUDE.md mcp/AGENTS.md mcp/CLAUDE.md ship-status-dev/AGENTS.md ship-status-dev/CLAUDE.md

verify-apm: apm
	@if [ -n "$$(git status --porcelain -- $(APM_GENERATED_FILES))" ]; then \
		echo "ERROR: Generated APM files are out of date. Run 'make apm' and commit the results."; \
		git status --short -- $(APM_GENERATED_FILES); \
		exit 1; \
	fi
