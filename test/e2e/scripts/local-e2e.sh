#!/bin/bash

set -e

POSTGRES_CONTAINER_NAME="ship-status-test-db"
PROMETHEUS_CONTAINER_NAME="prometheus-e2e"
DB_USER="postgres"
DB_PASSWORD="testpass"
DB_NAME="ship_status_e2e_$$"
# Devcontainer Postgres on ship-status-net (fixed host/port; see .devcontainer/init-services.sh).
E2E_PG_HOST=ship-status-postgres
E2E_PG_PORT=5432

# Detect whether to run services natively or in containers.
# PostgreSQL: that devcontainer service if reachable, otherwise a throwaway podman DB.
# Prometheus: use native binary if available, otherwise container.
USE_EXISTING_POSTGRES=false
USE_NATIVE_PROMETHEUS=false
STARTED_POSTGRES_CONTAINER=false

if command -v prometheus > /dev/null 2>&1; then
  USE_NATIVE_PROMETHEUS=true
fi

if command -v psql > /dev/null 2>&1; then
  if psql "postgres://$DB_USER:password@${E2E_PG_HOST}:${E2E_PG_PORT}/postgres?sslmode=disable" -c "SELECT 1" > /dev/null 2>&1; then
    USE_EXISTING_POSTGRES=true
  fi
fi

DOCKER=${DOCKER:-podman}
if [ "$USE_EXISTING_POSTGRES" = false ] || [ "$USE_NATIVE_PROMETHEUS" = false ]; then
  if ! command -v "$DOCKER" > /dev/null 2>&1; then
    echo "Missing native binaries and $DOCKER not available."
    [ "$USE_EXISTING_POSTGRES" = false ] && echo "  PostgreSQL: unreachable at ${E2E_PG_HOST}:${E2E_PG_PORT} (devcontainer) and no $DOCKER for a throwaway DB"
    [ "$USE_NATIVE_PROMETHEUS" = false ] && echo "  Prometheus: need prometheus binary OR $DOCKER"
    exit 1
  fi
fi

# If we need a container for PostgreSQL, find a free port
if [ "$USE_EXISTING_POSTGRES" = false ]; then
  DB_PORT=""
  for port in {5434..5450}; do
    if ! lsof -i :$port > /dev/null 2>&1; then
      DB_PORT=$port
      break
    fi
  done
  if [ -z "$DB_PORT" ]; then
    echo "No available port found in range 5434-5450 for PostgreSQL"
    exit 1
  fi
fi

NATIVE_PROMETHEUS_DATA=""

cleanup() {
  echo "Cleaning up component-monitor processes..."
  if [ ! -z "$COMPONENT_MONITOR_PID" ]; then
    kill -TERM $COMPONENT_MONITOR_PID 2>/dev/null || true
    sleep 1
    kill -KILL $COMPONENT_MONITOR_PID 2>/dev/null || true
  fi
  pkill -TERM -f "component-monitor.*e2e-component-monitor" 2>/dev/null || true
  sleep 1
  pkill -KILL -f "component-monitor.*e2e-component-monitor" 2>/dev/null || true

  echo "Stopping Prometheus..."
  if [ "$USE_NATIVE_PROMETHEUS" = true ]; then
    if [ ! -z "$PROMETHEUS_PID" ]; then
      kill -TERM $PROMETHEUS_PID 2>/dev/null || true
      sleep 1
      kill -KILL $PROMETHEUS_PID 2>/dev/null || true
    fi
    if [ ! -z "$NATIVE_PROMETHEUS_DATA" ] && [ -d "$NATIVE_PROMETHEUS_DATA" ]; then
      rm -rf "$NATIVE_PROMETHEUS_DATA"
    fi
  else
    if $DOCKER ps -a --format "{{.Names}}" 2>/dev/null | grep -q "^${PROMETHEUS_CONTAINER_NAME}$" 2>/dev/null; then
      $DOCKER stop "$PROMETHEUS_CONTAINER_NAME" > /dev/null 2>&1 || true
      $DOCKER rm -f "$PROMETHEUS_CONTAINER_NAME" > /dev/null 2>&1 || true
    fi
  fi

  echo "Cleaning up mock-monitored-component processes..."
  if [ ! -z "$MOCK_MONITORED_COMPONENT_PORT" ]; then
    lsof -ti :$MOCK_MONITORED_COMPONENT_PORT | xargs kill -TERM 2>/dev/null || true
    sleep 1
    lsof -ti :$MOCK_MONITORED_COMPONENT_PORT | xargs kill -KILL 2>/dev/null || true
  fi

  echo "Cleaning up proxy processes..."
  if [ ! -z "$PROXY_PORT" ]; then
    lsof -ti :$PROXY_PORT | xargs kill -TERM 2>/dev/null || true
    sleep 1
    lsof -ti :$PROXY_PORT | xargs kill -KILL 2>/dev/null || true
  fi

  echo "Cleaning up dashboard processes..."
  if [ ! -z "$DASHBOARD_PORT" ]; then
    lsof -ti :$DASHBOARD_PORT | xargs kill -TERM 2>/dev/null || true
    sleep 1
    lsof -ti :$DASHBOARD_PORT | xargs kill -KILL 2>/dev/null || true
  fi

  echo "Cleaning up postgres..."
  if [ "$USE_EXISTING_POSTGRES" = true ]; then
    psql "postgres://$DB_USER:password@${E2E_PG_HOST}:${E2E_PG_PORT}/postgres?sslmode=disable" -c "DROP DATABASE IF EXISTS $DB_NAME;" > /dev/null 2>&1 || true
  elif [ "$STARTED_POSTGRES_CONTAINER" = true ]; then
    $DOCKER rm -f $POSTGRES_CONTAINER_NAME > /dev/null 2>&1 || true
  fi

  echo "Cleaning up temporary files..."
  if [ ! -z "$DASHBOARD_CONFIG" ]; then
    rm -f "$DASHBOARD_CONFIG" 2>/dev/null || true
  fi
  if [ ! -z "$HMAC_SECRET_FILE" ] && [ -f "$HMAC_SECRET_FILE" ]; then
    rm -f "$HMAC_SECRET_FILE"
  fi
  if [ ! -z "$COMPONENT_MONITOR_CONFIG" ]; then
    rm -f "$COMPONENT_MONITOR_CONFIG" 2>/dev/null || true
  fi
  if [ ! -z "$PROMETHEUS_CONFIG_TMP" ] && [ -f "$PROMETHEUS_CONFIG_TMP" ]; then
    rm -f "$PROMETHEUS_CONFIG_TMP" 2>/dev/null || true
  fi
}

trap cleanup EXIT

# --- PostgreSQL ---
if [ "$USE_EXISTING_POSTGRES" = true ]; then
  echo "Using PostgreSQL at ${E2E_PG_HOST}:${E2E_PG_PORT} (ephemeral database $DB_NAME)..."
  echo "Creating test database $DB_NAME..."
  psql "postgres://$DB_USER:password@${E2E_PG_HOST}:${E2E_PG_PORT}/postgres?sslmode=disable" -c "DROP DATABASE IF EXISTS $DB_NAME;" > /dev/null 2>&1 || true
  psql "postgres://$DB_USER:password@${E2E_PG_HOST}:${E2E_PG_PORT}/postgres?sslmode=disable" -c "CREATE DATABASE $DB_NAME;" > /dev/null 2>&1
else
  echo "Starting PostgreSQL container on port $DB_PORT..."
  if $DOCKER ps -a --format "{{.Names}}" 2>/dev/null | grep -q "^${POSTGRES_CONTAINER_NAME}$" 2>/dev/null; then
    $DOCKER rm -f $POSTGRES_CONTAINER_NAME > /dev/null 2>&1 || true
  fi

  $DOCKER run -d \
    --name $POSTGRES_CONTAINER_NAME \
    -e POSTGRES_PASSWORD=$DB_PASSWORD \
    -p $DB_PORT:5432 \
    quay.io/enterprisedb/postgresql:latest
  STARTED_POSTGRES_CONTAINER=true

  echo "Waiting for PostgreSQL to be ready..."
  for i in {1..30}; do
    if $DOCKER exec $POSTGRES_CONTAINER_NAME pg_isready -U $DB_USER > /dev/null 2>&1; then
      echo "PostgreSQL is ready"
      break
    fi
    if [ $i -eq 30 ]; then
      echo "PostgreSQL failed to start"
      $DOCKER logs $POSTGRES_CONTAINER_NAME 2>/dev/null || true
      $DOCKER stop $POSTGRES_CONTAINER_NAME 2>/dev/null || true
      $DOCKER rm $POSTGRES_CONTAINER_NAME 2>/dev/null || true
      exit 1
    fi
    sleep 1
  done

  echo "Creating test database..."
  if ! $DOCKER exec $POSTGRES_CONTAINER_NAME psql -U $DB_USER -c "CREATE DATABASE $DB_NAME;" > /dev/null 2>&1; then
    echo "Failed to create test database (may already exist, continuing...)"
  fi
fi

if [ "$USE_EXISTING_POSTGRES" = true ]; then
  DSN="postgres://$DB_USER:password@${E2E_PG_HOST}:${E2E_PG_PORT}/$DB_NAME?sslmode=disable&client_encoding=UTF8"
else
  DSN="postgres://$DB_USER:$DB_PASSWORD@localhost:$DB_PORT/$DB_NAME?sslmode=disable&client_encoding=UTF8"
fi
export TEST_DATABASE_DSN="$DSN"

echo "Running migration..."
PROJECT_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
cd "$PROJECT_ROOT"
go run ./cmd/migrate --dsn "$DSN"

echo "Seeding SLO payloads..."
go run ./cmd/seed-slo --dsn "$DSN" --config test/e2e/scripts/dashboard-config.yaml

echo "Generating HMAC secret..."
HMAC_SECRET=$(openssl rand -hex 32)
HMAC_SECRET_FILE=$(mktemp)
echo -n "$HMAC_SECRET" > "$HMAC_SECRET_FILE"
echo "HMAC secret written to $HMAC_SECRET_FILE"

echo "Finding available ports..."
DASHBOARD_PORT=""
for port in {8080..8099}; do
  if ! lsof -i :$port > /dev/null 2>&1; then
    DASHBOARD_PORT=$port
    break
  fi
done

if [ -z "$DASHBOARD_PORT" ]; then
  echo "No available port found in range 8080-8099 for dashboard"
  exit 1
fi

PROXY_PORT=""
for port in {8443..8499}; do
  if ! lsof -i :$port > /dev/null 2>&1; then
    PROXY_PORT=$port
    break
  fi
done

if [ -z "$PROXY_PORT" ]; then
  echo "No available port found in range 8443-8499 for proxy"
  exit 1
fi

MOCK_MONITORED_COMPONENT_PORT="9000"
if lsof -i :$MOCK_MONITORED_COMPONENT_PORT > /dev/null 2>&1; then
  echo "Port $MOCK_MONITORED_COMPONENT_PORT is already in use for mock-monitored-component"
  exit 1
fi

PROMETHEUS_PORT=""
for port in {9090..9119}; do
  if ! lsof -i :$port > /dev/null 2>&1; then
    PROMETHEUS_PORT=$port
    break
  fi
done

if [ -z "$PROMETHEUS_PORT" ]; then
  echo "No available port found in range 9090-9119 for Prometheus"
  exit 1
fi

echo "Using port $DASHBOARD_PORT for dashboard server"
echo "Using port $PROXY_PORT for mock oauth-proxy"
echo "Using port $MOCK_MONITORED_COMPONENT_PORT for mock-monitored-component"
echo "Using port $PROMETHEUS_PORT for Prometheus"

echo "Starting dashboard server..."
DASHBOARD_PID=""
DASHBOARD_LOG="/tmp/dashboard-server.log"

# Create temporary dashboard config file so tests don't modify the original
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DASHBOARD_CONFIG=$(mktemp)
cp "$SCRIPT_DIR/dashboard-config.yaml" "$DASHBOARD_CONFIG"
export TEST_DASHBOARD_CONFIG_PATH="$DASHBOARD_CONFIG"

# Start dashboard server in background
unset SKIP_AUTH # make sure we are using authentication
go run ./cmd/dashboard --config "$DASHBOARD_CONFIG" --port $DASHBOARD_PORT --dsn "$DSN" --hmac-secret-file "$HMAC_SECRET_FILE" --absent-report-check-interval 15s --trt-payload-prune-interval 15s --config-update-poll-interval 10s --slack-base-url "http://localhost:3030" --slack-workspace-url "https://rhsandbox.slack.com/" 2> "$DASHBOARD_LOG" &
DASHBOARD_PID=$!

# Wait for dashboard server to be ready
echo "Waiting for dashboard server to be ready..."
for i in {1..30}; do
  if curl -s http://localhost:$DASHBOARD_PORT/health > /dev/null 2>&1; then
    echo "Dashboard server is ready on port $DASHBOARD_PORT"
    break
  fi
  if [ $i -eq 30 ]; then
    echo "Dashboard server failed to start"
    echo "=== Server Log ==="
    cat "$DASHBOARD_LOG" 2>/dev/null || echo "No log found"
    if [ ! -z "$DASHBOARD_PORT" ]; then
      lsof -ti :$DASHBOARD_PORT | xargs kill -KILL 2>/dev/null || true
    fi
    exit 1
  fi
  sleep 1
done

echo "Starting mock oauth-proxy..."
PROXY_PID=""
PROXY_LOG="/tmp/mock-oauth-proxy.log"

# Start mock oauth-proxy in background
go run ./cmd/mock-oauth-proxy --config test/e2e/scripts/mock-oauth-proxy-config.yaml --port $PROXY_PORT --upstream "http://localhost:$DASHBOARD_PORT" --hmac-secret-file "$HMAC_SECRET_FILE" 2> "$PROXY_LOG" &
PROXY_PID=$!

# Wait for proxy to be ready
echo "Waiting for mock oauth-proxy to be ready..."
for i in {1..30}; do
  if curl -s http://localhost:$PROXY_PORT/health > /dev/null 2>&1; then
    echo "Mock oauth-proxy is ready on port $PROXY_PORT"
    break
  fi
  if [ $i -eq 30 ]; then
    echo "Mock oauth-proxy failed to start"
    echo "=== Proxy Log ==="
    cat "$PROXY_LOG" 2>/dev/null || echo "No log found"
    if [ ! -z "$PROXY_PORT" ]; then
      lsof -ti :$PROXY_PORT | xargs kill -KILL 2>/dev/null || true
    fi
    exit 1
  fi
  sleep 1
done

echo "Starting mock-monitored-component..."
# Start mock-monitored-component in background
go run ./cmd/mock-monitored-component --port $MOCK_MONITORED_COMPONENT_PORT > /dev/null 2>&1 &

# Wait for mock-monitored-component to be ready
echo "Waiting for mock-monitored-component to be ready..."
for i in {1..30}; do
  if curl -s http://localhost:$MOCK_MONITORED_COMPONENT_PORT/health > /dev/null 2>&1; then
    echo "Mock-monitored-component is ready on port $MOCK_MONITORED_COMPONENT_PORT"
    break
  fi
  if [ $i -eq 30 ]; then
    echo "Mock-monitored-component failed to start"
    exit 1
  fi
  sleep 1
done

# --- Prometheus ---
PROMETHEUS_CONFIG_PATH="$(cd "$(dirname "$0")" && pwd)/prometheus.yml"
PROMETHEUS_CONFIG_TMP=$(mktemp)

PROMETHEUS_LOG="/tmp/prometheus-e2e.log"

if [ "$USE_NATIVE_PROMETHEUS" = true ]; then
  echo "Starting Prometheus (native) on port $PROMETHEUS_PORT..."
  NATIVE_PROMETHEUS_DATA=$(mktemp -d)

  MOCK_MONITORED_COMPONENT_TARGET="localhost:${MOCK_MONITORED_COMPONENT_PORT}"
  export MOCK_MONITORED_COMPONENT_TARGET
  envsubst < "$PROMETHEUS_CONFIG_PATH" > "$PROMETHEUS_CONFIG_TMP"

  prometheus \
    --config.file="$PROMETHEUS_CONFIG_TMP" \
    --storage.tsdb.path="$NATIVE_PROMETHEUS_DATA" \
    --web.listen-address=":$PROMETHEUS_PORT" \
    --web.enable-lifecycle \
    > "$PROMETHEUS_LOG" 2>&1 &
  PROMETHEUS_PID=$!
else
  echo "Starting Prometheus in container on port $PROMETHEUS_PORT..."

  # Detect Docker vs Podman for correct host alias
  DOCKER_BASENAME=$(basename "$DOCKER")
  if [ "$DOCKER_BASENAME" = "docker" ]; then
    CONTAINER_HOST="host.docker.internal"
    HOST_FLAG="--add-host=host.docker.internal:host-gateway"
  else
    CONTAINER_HOST="host.containers.internal"
    HOST_FLAG=""
  fi

  MOCK_MONITORED_COMPONENT_TARGET="${CONTAINER_HOST}:${MOCK_MONITORED_COMPONENT_PORT}"
  export MOCK_MONITORED_COMPONENT_TARGET
  envsubst < "$PROMETHEUS_CONFIG_PATH" > "$PROMETHEUS_CONFIG_TMP"

  if $DOCKER ps -a --format "{{.Names}}" 2>/dev/null | grep -q "^${PROMETHEUS_CONTAINER_NAME}$" 2>/dev/null; then
    $DOCKER rm -f "$PROMETHEUS_CONTAINER_NAME" > /dev/null 2>&1 || true
  fi

  $DOCKER run -d \
    --name "$PROMETHEUS_CONTAINER_NAME" \
    $HOST_FLAG \
    -p $PROMETHEUS_PORT:9090 \
    -v "$PROMETHEUS_CONFIG_TMP:/etc/prometheus/prometheus.yml:ro" \
    quay.io/prometheus/prometheus:latest \
    --config.file=/etc/prometheus/prometheus.yml \
    --storage.tsdb.path=/prometheus \
    --web.console.libraries=/usr/share/prometheus/console_libraries \
    --web.console.templates=/usr/share/prometheus/consoles \
    --web.enable-lifecycle \
    > /dev/null 2>&1
fi

echo "Waiting for Prometheus to complete initial scrape..."
for i in {1..60}; do
  if curl -s "http://localhost:$PROMETHEUS_PORT/api/v1/query?query=success_rate" | grep -q "success_rate"; then
    echo "Prometheus has completed initial scrape"
    break
  fi
  if [ $i -eq 60 ]; then
    echo "Prometheus failed to complete initial scrape within 60 seconds"
    if [ "$USE_NATIVE_PROMETHEUS" = true ]; then
      echo "=== Prometheus Log (last 30 lines) ==="
      tail -n 30 "$PROMETHEUS_LOG" 2>/dev/null || echo "No log found"
    else
      $DOCKER logs "$PROMETHEUS_CONTAINER_NAME" 2>/dev/null || true
    fi
    exit 1
  fi
  sleep 1
done

COMPONENT_MONITOR_HEALTH_PORT=""
for port in {8100..8119}; do
  if ! lsof -i :$port > /dev/null 2>&1; then
    COMPONENT_MONITOR_HEALTH_PORT=$port
    break
  fi
done

if [ -z "$COMPONENT_MONITOR_HEALTH_PORT" ]; then
  echo "No available port found in range 8100-8119 for component-monitor health"
  exit 1
fi

echo "Using port $COMPONENT_MONITOR_HEALTH_PORT for component-monitor health"

echo "Starting component-monitor..."
COMPONENT_MONITOR_LOG="/tmp/component-monitor.log"

# Export environment variables for config substitution
export TEST_SERVER_URL="http://localhost:$DASHBOARD_PORT"
export TEST_MOCK_OAUTH_PROXY_URL="http://localhost:$PROXY_PORT"
export TEST_MOCK_MONITORED_COMPONENT_URL="http://localhost:$MOCK_MONITORED_COMPONENT_PORT"
export TEST_PROMETHEUS_URL="http://localhost:$PROMETHEUS_PORT"

# Create temporary config file with substituted values
COMPONENT_MONITOR_CONFIG=$(mktemp)
envsubst < test/e2e/scripts/component-monitor-config.yaml > "$COMPONENT_MONITOR_CONFIG"
export TEST_COMPONENT_MONITOR_CONFIG_PATH="$COMPONENT_MONITOR_CONFIG"

# Create temporary token file for component-monitor authentication
COMPONENT_MONITOR_TOKEN=$(mktemp)
echo "component-monitor-sa-token" > "$COMPONENT_MONITOR_TOKEN"

# Start component-monitor in background
go run ./cmd/component-monitor --config-path "$COMPONENT_MONITOR_CONFIG" --dashboard-url "$TEST_MOCK_OAUTH_PROXY_URL" --name "e2e-component-monitor" --report-auth-token-file "$COMPONENT_MONITOR_TOKEN" --config-update-poll-interval 10s --health-port "$COMPONENT_MONITOR_HEALTH_PORT" 2> "$COMPONENT_MONITOR_LOG" &
COMPONENT_MONITOR_PID=$!

echo "Waiting for component-monitor to be ready..."
for i in {1..30}; do
  if curl -sf http://localhost:$COMPONENT_MONITOR_HEALTH_PORT/health > /dev/null 2>&1; then
    echo "Component-monitor is ready (pid $COMPONENT_MONITOR_PID)"
    break
  fi
  if [ $i -eq 30 ]; then
    echo "Component-monitor failed to become ready within 30 seconds"
    tail -n 30 "$COMPONENT_MONITOR_LOG" 2>/dev/null || echo "No log found"
    exit 1
  fi
  sleep 1
done

echo "Running e2e tests..."
set +e
gotestsum --format testname ./test/e2e/... -count 1 -p 1
TEST_EXIT_CODE=$?
set -e

# Only show logs if tests failed
if [ $TEST_EXIT_CODE -ne 0 ]; then
  echo ""
  echo "=== Component Monitor Log (last 50 lines) ==="
  tail -n 50 "$COMPONENT_MONITOR_LOG" 2>/dev/null || echo "No log found"
  echo "Full log: $COMPONENT_MONITOR_LOG"

  echo ""
  echo "=== Dashboard Server Log (last 50 lines) ==="
  tail -n 50 "$DASHBOARD_LOG" 2>/dev/null || echo "No log found"
  echo "Full log: $DASHBOARD_LOG"

  echo ""
  echo "=== Mock OAuth Proxy Log (last 50 lines) ==="
  tail -n 50 "$PROXY_LOG" 2>/dev/null || echo "No log found"
  echo "Full log: $PROXY_LOG"
  echo ""
fi
if [ $TEST_EXIT_CODE -eq 0 ]; then
  echo "✓ Tests passed"
else
  echo "✗ Tests failed with exit code $TEST_EXIT_CODE"
fi

exit $TEST_EXIT_CODE
