# Development Guide

This document describes how to set up and run the Ship Status Dashboard for local development and testing.

For production layout and authentication, see [ARCHITECTURE.md](ARCHITECTURE.md).

## Dev container (recommended)

The preferred way to develop is the [dev container](.devcontainer/README.md). It provides a consistent environment (Go, Node, tooling, PostgreSQL) and matches the ports used across the project.

**Prerequisites:** Podman v4+ or Docker with Compose, plus the [devcontainer CLI](https://containers.dev/supporting). See [.devcontainer/README.md](.devcontainer/README.md) for platform-specific `devcontainer up` commands (Podman on macOS/Linux, Docker Desktop, and cleanup).

**Quick start:**

1. Open the repository in VS Code or Cursor and **Reopen in Container** (or run `devcontainer up --workspace-folder .` from the repo root).
2. Copy [`.devcontainer/.env.example`](.devcontainer/.env.example) to `.devcontainer/.env` if you need to override defaults.
3. On first create, `post-create.sh` installs dependencies and runs migrations against the bundled PostgreSQL service.

**Services** (see the [dev container README](.devcontainer/README.md) for full detail):

| Service | Port | How to start |
|---------|------|----------------|
| PostgreSQL | 5433 (host) | Started automatically by `init-services.sh` |
| Dashboard API | 8180 | `/ship-status-dev-serve` or the `dashboard_serve` MCP tool |
| Mock OAuth Proxy | 8443 | Started with the dashboard |
| Vite dev server | 3030 | `/ship-status-dev-frontend` or the `frontend_serve` MCP tool |
| Prometheus | 9090 | `/ship-status-dev-app` when testing component-monitor |

Default database URL inside the container: `postgres://postgres:password@ship-status-postgres:5432/ship_status?sslmode=disable` (`SHIP_STATUS_DSN`).

In Cursor/Claude Code, slash commands such as `/ship-status-dev-setup`, `/ship-status-dev-serve`, and `/ship-status-dev-frontend` wrap the same workflow; see [`.devcontainer/README.md`](.devcontainer/README.md).

---

## Manual setup (optional)

Use this path if you are not using the dev container—for example, running services directly on the host with your own PostgreSQL instance.

### Prerequisites

Before starting the dashboard, set up a PostgreSQL database:

1. Start a PostgreSQL container:
   ```bash
   podman run -d \
     --name ship-status-db \
     -e POSTGRES_PASSWORD=yourpassword \
     -p 5432:5432 \
     quay.io/enterprisedb/postgresql:latest
   ```

2. Create the database:
   ```bash
   podman exec ship-status-db psql -U postgres -c "CREATE DATABASE ship_status;"
   ```

### Backend

#### Dashboard

Start the dashboard server and mock oauth-proxy using the local development script:

```bash
./hack/local/dashboard/local-dev.sh "postgres://postgres:yourpassword@localhost:5432/ship_status?sslmode=disable"
```

This script:
- Starts the dashboard server on port 8180 (public route, no auth)
- Starts the mock oauth-proxy on port 8443 (protected route, requires basic auth)
- Sets up a user with credentials: `developer:password`
- Generates a temporary HMAC secret for request signing
- After migrate, runs `go run ./cmd/seed-slo --dsn "$DSN" --config hack/local/dashboard/config.yaml` so `/team/TRT` has a current SLO workspace

Re-seed without restarting the dashboard:

```bash
go run ./cmd/seed-slo --dsn "postgres://postgres:yourpassword@localhost:5432/ship_status?sslmode=disable" --config hack/local/dashboard/config.yaml
```

`--dsn` is required. `--config` defaults to `hack/local/dashboard/config.yaml`. E2e passes `test/e2e/scripts/dashboard-config.yaml`.

**Slack Integration**: To enable Slack integration for outage reporting, set the `SLACK_BOT_TOKEN` environment variable before running the script:

```bash
SLACK_BOT_TOKEN=xoxb-your-token ./hack/local/dashboard/local-dev.sh "postgres://postgres:yourpassword@localhost:5432/ship_status?sslmode=disable"
```

#### Component Monitor

Start the component-monitor using the local development script:

```bash
./hack/local/component-monitor/local-dev.sh
```

This script:
- Starts a mock-monitored-component on port 8081
- Starts Prometheus in a podman container on port 9090
- Starts the component-monitor with the local configuration

**Note**: The component-monitor expects the dashboard to be running on `http://localhost:8180`. Make sure to start the dashboard first.

### Authentication Architecture

The dashboard local development script mimics the production architecture using `mock-oauth-proxy`:

#### Setup

1. **Dashboard Server** (port 8180)
   - Runs with full authentication enabled (no `SKIP_AUTH`)
   - Public routes are accessible without authentication (same as production)
   - Protected routes require authentication via mock-oauth-proxy

2. **Mock OAuth Proxy** (port 8443)
   - Implements basic authentication (username/password)
   - Proxies to dashboard on `localhost:8180`
   - Adds same headers as production oauth-proxy
   - Signs requests with HMAC using shared secret
   - Default credentials: `developer:password`

#### Architecture

```
Public Route:    http://localhost:8180 → Dashboard (no auth required)
Protected Route: http://localhost:8443 → Mock OAuth Proxy → Dashboard (localhost:8180)
```

**Note**: The `SKIP_AUTH=1` environment variable is available for a simpler (unrecommended) development setup without oauth-proxy, but the local development script uses the full authentication flow with mock-oauth-proxy to accurately mirror production.

The mock-oauth-proxy:
- Accepts Basic Auth credentials
- Validates against [YAML user configuration](hack/local/dashboard/mock-oauth-proxy-config.yaml)
- Forwards authenticated requests to dashboard
- Adds `X-Forwarded-User`, `X-Forwarded-Email`, and signs with HMAC

#### Shared HMAC Secret

Both processes use the same HMAC secret:
- Generated automatically by `hack/local/dashboard/local-dev.sh`
- Stored in temporary file
- Passed to both dashboard and mock-oauth-proxy via `--hmac-secret-file`

### Frontend

1. Navigate to the frontend directory:
   ```bash
   cd frontend
   ```

2. Install dependencies:
   ```bash
   npm ci --ignore-scripts
   ```

3. Start the development server. [`frontend/.env.development`](frontend/.env.development) already sets `VITE_PUBLIC_DOMAIN` and `VITE_PROTECTED_DOMAIN` for local URLs; override inline if needed:
   ```bash
   VITE_PUBLIC_DOMAIN=http://localhost:8180 \
   VITE_PROTECTED_DOMAIN=http://localhost:3030 \
   npm start
   ```

The frontend will be available at `http://localhost:3030`.

In development, Login and other protected calls use that same origin. Vite proxies `/oauth` and `/api` to the mock oauth proxy on port 8443, so the browser does not need to open port 8443. The basic-auth prompt still uses `developer:password`.

---

## End-to-End Tests

The e2e test suite (`make local-e2e`) tests both the dashboard and component-monitor using the same architecture as local development:

- Dynamically assigns ports (8080-8099 for dashboard, 8443-8499 for proxy)
- Starts dashboard server without `SKIP_AUTH` (full authentication enabled)
- Starts mock-oauth-proxy with test user credentials
- Starts mock-monitored-component for component-monitor testing
- Starts component-monitor with test configuration
- Tests both public and protected routes
- Uses same HMAC signature verification as production

### Running E2E Tests

Run the complete e2e test suite:

```bash
make local-e2e
```

The e2e script (`test/e2e/scripts/local-e2e.sh`):
- Starts a PostgreSQL test container using podman
- Runs database migrations
- Seeds the TRT SLO workspace from `test/e2e/scripts/dashboard-config.yaml`
- Starts the dashboard server on a dynamically assigned port (8080-8099)
- Starts the mock oauth-proxy on a dynamically assigned port (8443-8499)
- Starts the mock-monitored-component on port 9000
- Starts the component-monitor with test configuration
- Executes both dashboard and component-monitor e2e test suites
- Cleans up all processes and containers on completion

The test suite includes:
- **Dashboard tests** (`TestE2E_Dashboard` in `dashboard_test.go`): Tests API endpoints, outages, component status, and user authentication
- **Component-monitor report tests** (`TestE2E_ComponentMonitorReport` in `component-monitor-report_test.go`): Tests the dashboard `/api/component-monitor/report` endpoint, including per-reason outages
- **Component-monitor tests** (`TestE2E_ComponentMonitor` in `component-monitor_test.go`): Tests component monitoring probes and integration with the dashboard

### Frontend BDD Tests

Run the Playwright BDD test suite against the frontend:

```bash
make bdd
```

This starts the Vite dev server, launches headless Chromium, and runs all BDD scenarios defined in `frontend/bdd/features/`. Tests use mocked API responses and require no backend.
