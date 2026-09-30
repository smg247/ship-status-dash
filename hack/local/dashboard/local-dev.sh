#!/bin/bash

set -e

BACKGROUND=false
if [ "$1" = "--background" ]; then
  BACKGROUND=true
  shift
fi

if [ -z "$1" ]; then
  echo "Usage: $0 [--background] <database-dsn>"
  echo "Example: $0 'postgres://user:pass@localhost:5432/ship_status?sslmode=disable'"
  echo ""
  echo "Options:"
  echo "  --background  Start services and exit (no interactive wait/cleanup)"
  exit 1
fi

DSN="$1"

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$PROJECT_ROOT"

LOG_DIR="${SHIP_STATUS_LOG_DIR:-/tmp}"
mkdir -p "$LOG_DIR"

kill_processes_on_port() {
  local port=$1
  local message=${2:-"Stopping processes on port $port..."}

  PIDS=$(lsof -ti :$port 2>/dev/null) || true
  if [ ! -z "$PIDS" ]; then
    echo "$message"
    for pid in $PIDS; do
      kill -TERM "$pid" 2>/dev/null || true
    done
    sleep 1
    PIDS=$(lsof -ti :$port 2>/dev/null) || true
    if [ ! -z "$PIDS" ]; then
      for pid in $PIDS; do
        kill -KILL "$pid" 2>/dev/null || true
      done
    fi
  fi
}

DASHBOARD_PORT="${DASHBOARD_PORT:-8180}"
PROXY_PORT="${PROXY_PORT:-8443}"

echo "Ensuring ports $DASHBOARD_PORT and $PROXY_PORT are free..."
kill_processes_on_port "$DASHBOARD_PORT" "Stopping processes on port $DASHBOARD_PORT..."
kill_processes_on_port "$PROXY_PORT" "Stopping processes on port $PROXY_PORT..."

echo "Checking if ports are available..."
if lsof -i :$DASHBOARD_PORT > /dev/null 2>&1; then
  echo "Error: Port $DASHBOARD_PORT is already in use"
  exit 1
fi

if lsof -i :$PROXY_PORT > /dev/null 2>&1; then
  echo "Error: Port $PROXY_PORT is already in use"
  exit 1
fi

if [ "$BACKGROUND" = false ]; then
  cleanup() {
    local exit_status=$?
    set +e
    echo ""
    echo "Cleaning up..."

    PROXY_PORT=${PROXY_PORT:-8443}
    DASHBOARD_PORT=${DASHBOARD_PORT:-8180}

    if [ ! -z "$TAIL_PID" ]; then
      kill "$TAIL_PID" 2>/dev/null || true
    fi

    kill_processes_on_port "$PROXY_PORT"
    kill_processes_on_port "$DASHBOARD_PORT"

    if [ ! -z "$HMAC_SECRET_FILE" ] && [ -f "$HMAC_SECRET_FILE" ]; then
      rm -f "$HMAC_SECRET_FILE"
    fi

    echo "Cleanup complete"
    exit "$exit_status"
  }

  trap cleanup EXIT
fi

echo "Running database migrations..."
if ! go run ./cmd/migrate --dsn "$DSN"; then
  echo "Error: Database migration failed"
  exit 1
fi

echo "Seeding TRT SLO payloads..."
if ! go run ./cmd/seed-slo --dsn "$DSN" --config hack/local/dashboard/config.yaml; then
  echo "Error: TRT SLO seed failed"
  exit 1
fi

echo "Generating HMAC secret..."
HMAC_SECRET=$(openssl rand -hex 32)
HMAC_SECRET_FILE=$(mktemp)
echo -n "$HMAC_SECRET" > "$HMAC_SECRET_FILE"
echo "HMAC secret written to $HMAC_SECRET_FILE"

echo "Starting dashboard server..."
DASHBOARD_LOG="$LOG_DIR/dashboard-local-dev.log"
echo "Dashboard server logs: $DASHBOARD_LOG"

go run ./cmd/dashboard --config hack/local/dashboard/config.yaml --port $DASHBOARD_PORT --dsn "$DSN" --hmac-secret-file "$HMAC_SECRET_FILE" --cors-origin "http://localhost:3030" --absent-report-check-interval 15s --trt-payload-prune-interval 15s --slack-base-url "http://localhost:3030" > "$DASHBOARD_LOG" 2>&1 &
DASHBOARD_PID=$!

echo "Waiting for dashboard server to be ready..."
for i in {1..30}; do
  if curl -s http://localhost:$DASHBOARD_PORT/health > /dev/null 2>&1; then
    echo "Dashboard server is ready on port $DASHBOARD_PORT (pid $DASHBOARD_PID)"
    break
  fi
  if [ $i -eq 30 ]; then
    echo "Dashboard server failed to start"
    echo "=== Server Log ==="
    cat "$DASHBOARD_LOG" 2>/dev/null || echo "No log found"
    kill_processes_on_port "$DASHBOARD_PORT"
    exit 1
  fi
  sleep 1
done

echo "Starting mock oauth-proxy..."
PROXY_LOG="$LOG_DIR/mock-oauth-proxy-local-dev.log"
echo "Mock oauth-proxy logs: $PROXY_LOG"

go run ./cmd/mock-oauth-proxy --config hack/local/dashboard/mock-oauth-proxy-config.yaml --port $PROXY_PORT --upstream "http://localhost:$DASHBOARD_PORT" --hmac-secret-file "$HMAC_SECRET_FILE" > "$PROXY_LOG" 2>&1 &
PROXY_PID=$!

echo "Waiting for mock oauth-proxy to be ready..."
for i in {1..30}; do
  if curl -s http://localhost:$PROXY_PORT/health > /dev/null 2>&1; then
    echo "Mock oauth-proxy is ready on port $PROXY_PORT (pid $PROXY_PID)"
    break
  fi
  if [ $i -eq 30 ]; then
    echo "Mock oauth-proxy failed to start"
    echo "=== Proxy Log ==="
    cat "$PROXY_LOG" 2>/dev/null || echo "No log found"
    kill_processes_on_port "$PROXY_PORT"
    kill_processes_on_port "$DASHBOARD_PORT"
    exit 1
  fi
  sleep 1
done

echo ""
echo "Local development environment is ready!"
echo ""
echo "Routes (matching production setup):"
echo "  Public Route (no auth):     http://localhost:$DASHBOARD_PORT"
echo "  Protected Route (with auth): http://localhost:$PROXY_PORT"
echo ""
echo "Credentials: developer:password"
echo "HMAC secret: $HMAC_SECRET_FILE"
echo ""
echo "Log files:"
echo "  Dashboard server: $DASHBOARD_LOG"
echo "  Mock oauth-proxy: $PROXY_LOG"

if [ "$BACKGROUND" = true ]; then
  echo ""
  echo "PIDs: dashboard=$DASHBOARD_PID proxy=$PROXY_PID"
  exit 0
fi

echo ""
echo "Starting to tail dashboard logs..."
tail -f "$DASHBOARD_LOG" &
TAIL_PID=$!

echo "Press Ctrl+C to stop"
echo ""

set +e
while kill -0 $DASHBOARD_PID 2>/dev/null && kill -0 $PROXY_PID 2>/dev/null; do
  sleep 1
done
