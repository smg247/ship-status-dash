---
description: "Run lint and unit tests (full local CI suite, excluding e2e)"
---

# Ship Status dev — tests

Use the **`run_tests`** MCP tool (server: **`ship-status-dev`**). This runs two steps in order, stopping if any step fails:

1. **Lint** — `make lint`
   - `hack/go-lint.sh` uses the locally installed `golangci-lint` if available, otherwise falls back to a container.

2. **Unit tests** — `make test`
   - Runs frontend BDD tests first (via `make bdd` dependency), then Go tests via gotestsum.
   - To run only frontend BDD tests, use `/ship-status-dev-bdd`.

This does **not** run e2e tests. Use `/ship-status-dev-e2e` for that. It does **not** run `make verify-apm`. That check compares generated APM files to HEAD and belongs in CI.

Logs: **`ship-status-dev-logs/run_lint.log`** and **`ship-status-dev-logs/run_test.log`**.
