import os
import signal
import subprocess
import sys
import time
import urllib.request
from collections.abc import Callable
from pathlib import Path
from typing import NamedTuple

from fastmcp import FastMCP

mcp = FastMCP("ship-status-dev")

REPO_ROOT = Path(__file__).resolve().parent.parent
DEV_LOG_DIR = REPO_ROOT / "ship-status-dev-logs"

_MAX_TOOL_CHARS = 28000


def _ensure_dev_log_dir() -> None:
    DEV_LOG_DIR.mkdir(parents=True, exist_ok=True)


def _tail_file(path: Path, max_lines: int) -> str:
    try:
        lines = path.read_text(encoding="utf-8", errors="replace").splitlines()
    except OSError as e:
        return f"(could not read log: {e})"
    return "\n".join(lines[-max_lines:])


def _free_tcp_port(port: int) -> None:
    """SIGTERM then SIGKILL listeners on ``port`` (matches hack/local dashboard port cleanup)."""
    script = (
        f"pids=$(lsof -ti :{port} 2>/dev/null); "
        f'[ -n "$pids" ] && for pid in $pids; do kill -TERM "$pid" 2>/dev/null; done; '
        f"sleep 1; "
        f"pids=$(lsof -ti :{port} 2>/dev/null); "
        f'[ -n "$pids" ] && for pid in $pids; do kill -KILL "$pid" 2>/dev/null; done; '
        f"true"
    )
    try:
        subprocess.run(
            ["bash", "-c", script],
            timeout=15,
            cwd=str(REPO_ROOT),
            capture_output=True,
        )
    except (subprocess.TimeoutExpired, FileNotFoundError, OSError):
        pass


def _terminate_process_group(proc: subprocess.Popen) -> None:
    """Best-effort teardown for Popen with start_new_session=True (session leader = process group)."""
    if proc.poll() is not None:
        return
    pid = proc.pid
    if pid is None:
        return
    killpg = getattr(os, "killpg", None)
    if killpg is not None:
        try:
            killpg(pid, signal.SIGTERM)
        except (ProcessLookupError, ChildProcessError, OSError):
            try:
                proc.terminate()
            except ProcessLookupError:
                return
    else:
        try:
            proc.terminate()
        except ProcessLookupError:
            return
    for _ in range(20):
        if proc.poll() is not None:
            return
        time.sleep(0.1)
    if proc.poll() is not None:
        return
    if killpg is not None:
        try:
            killpg(pid, signal.SIGKILL)
        except (ProcessLookupError, ChildProcessError, OSError):
            try:
                proc.kill()
            except ProcessLookupError:
                return
    else:
        try:
            proc.kill()
        except ProcessLookupError:
            return
    try:
        proc.wait(timeout=5)
    except subprocess.TimeoutExpired:
        pass


def _default_dsn() -> str:
    return os.environ.get(
        "SHIP_STATUS_DSN",
        "postgres://postgres:password@localhost:5433/ship_status?sslmode=disable",
    )


# ---------------------------------------------------------------------------
# Process detection helpers
# ---------------------------------------------------------------------------

def _pgrep_pids(pattern: str) -> list[int]:
    try:
        r = subprocess.run(
            ["pgrep", "-f", pattern],
            capture_output=True,
            text=True,
            timeout=5,
        )
    except (FileNotFoundError, subprocess.TimeoutExpired):
        return []
    if r.returncode != 0:
        return []
    return [int(x) for x in r.stdout.split() if x.strip().isdigit()]


def _filter_pids_by_cwd(pids: list[int], expected_cwd: Path) -> list[int]:
    if not pids:
        return []
    filtered: list[int] = []
    for pid in pids:
        try:
            r = subprocess.run(
                ["lsof", "-a", "-p", str(pid), "-d", "cwd", "-Fn"],
                capture_output=True,
                text=True,
                timeout=5,
            )
        except (FileNotFoundError, subprocess.TimeoutExpired):
            return pids
        for line in r.stdout.splitlines():
            if line.startswith("n") and Path(line[1:]).resolve() == expected_cwd:
                filtered.append(pid)
                break
    return filtered


def _proc_cmdline(pid_dir: Path) -> str:
    raw = (pid_dir / "cmdline").read_bytes()
    return raw.replace(b"\0", b" ").decode(errors="replace")


def _proc_cwd(pid_dir: Path) -> Path | None:
    try:
        return (pid_dir / "cwd").resolve()
    except OSError:
        return None


def _find_pids(
    expected_cwd: Path,
    cmdline_match: Callable[[str], bool],
    pgrep_patterns: list[str],
) -> list[int]:
    found: list[int] = []
    if sys.platform.startswith("linux"):
        for pid_dir in Path("/proc").iterdir():
            if not pid_dir.name.isdigit():
                continue
            try:
                if _proc_cwd(pid_dir) != expected_cwd:
                    continue
                cmd = _proc_cmdline(pid_dir)
            except OSError:
                continue
            if cmdline_match(cmd):
                found.append(int(pid_dir.name))
        if found:
            return sorted(set(found))
    for pat in pgrep_patterns:
        p = _filter_pids_by_cwd(_pgrep_pids(pat), expected_cwd)
        if p:
            return sorted(set(p))
    return []


def _stop_pids(pids: list[int]) -> str:
    for pid in pids:
        try:
            os.kill(pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
    time.sleep(1)
    killed = []
    for pid in pids:
        try:
            os.kill(pid, 0)
            os.kill(pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        killed.append(pid)
    return ", ".join(str(p) for p in killed)


def _pids_dashboard() -> list[int]:
    def _match(cmd: str) -> bool:
        return "cmd/dashboard" in cmd and "--config" in cmd
    return _find_pids(REPO_ROOT.resolve(), _match, ["cmd/dashboard"])


def _pids_oauth_proxy() -> list[int]:
    def _match(cmd: str) -> bool:
        return "cmd/mock-oauth-proxy" in cmd
    return _find_pids(REPO_ROOT.resolve(), _match, ["cmd/mock-oauth-proxy"])


def _pids_frontend() -> list[int]:
    def _match(cmd: str) -> bool:
        return "vite" in cmd or "npm" in cmd
    return _find_pids((REPO_ROOT / "frontend").resolve(), _match, ["vite"])


def _pids_mock_component() -> list[int]:
    def _match(cmd: str) -> bool:
        return "cmd/mock-monitored-component" in cmd
    return _find_pids(REPO_ROOT.resolve(), _match, ["cmd/mock-monitored-component"])


def _pids_prometheus() -> list[int]:
    def _match(cmd: str) -> bool:
        return "prometheus" in cmd and "--config.file" in cmd
    return _find_pids(REPO_ROOT.resolve(), _match, ["prometheus.*--config.file"])


def _pids_component_monitor() -> list[int]:
    # go run leaves a compiled child whose argv is .../component-monitor, not cmd/component-monitor.
    def _match(cmd: str) -> bool:
        return "cmd/component-monitor" in cmd or "--name local-component-monitor" in cmd

    return _find_pids(
        REPO_ROOT.resolve(),
        _match,
        ["cmd/component-monitor", "local-component-monitor"],
    )


# ---------------------------------------------------------------------------
# Script runner — calls hack/local/ scripts with --background
# ---------------------------------------------------------------------------

def _run_script_background(
    label: str,
    script: str,
    args: list[str],
    log_path: Path,
    ready_url: str | None = None,
    ready_timeout: int = 120,
    env_extra: dict[str, str] | None = None,
) -> str:
    """Run a shell script with --background, capturing output to log_path."""
    _ensure_dev_log_dir()
    log_path.parent.mkdir(parents=True, exist_ok=True)

    script_path = REPO_ROOT / script
    if not script_path.is_file():
        return f"script not found: {script_path}"

    run_env = os.environ.copy()
    run_env["SHIP_STATUS_LOG_DIR"] = str(DEV_LOG_DIR)
    if env_extra:
        run_env.update(env_extra)

    cmd = ["bash", str(script_path), "--background"] + args

    with open(log_path, "w", encoding="utf-8") as logf:
        try:
            r = subprocess.run(
                cmd,
                cwd=REPO_ROOT,
                env=run_env,
                stdout=logf,
                stderr=subprocess.STDOUT,
                stdin=subprocess.DEVNULL,
                timeout=ready_timeout,
            )
        except subprocess.TimeoutExpired:
            tail = _tail_file(log_path, 40)
            return f"{label} timed out after {ready_timeout}s. log: {log_path}\n--- tail ---\n{tail}"

    output = log_path.read_text(encoding="utf-8", errors="replace")

    if r.returncode != 0:
        tail = _tail_file(log_path, 60)
        return f"{label} failed (exit {r.returncode}). log: {log_path}\n--- tail ---\n{tail}"

    if ready_url:
        ready = False
        deadline = time.monotonic() + ready_timeout
        while time.monotonic() < deadline:
            try:
                urllib.request.urlopen(ready_url, timeout=2)
                ready = True
                break
            except Exception:
                time.sleep(1)
        if not ready:
            tail = _tail_file(log_path, 40)
            return f"{label} started but readiness probe failed ({ready_url}). log: {log_path}\n--- tail ---\n{tail}"

    return f"{label} started. log: {log_path}\n{output}"


class ForegroundRunResult(NamedTuple):
    success: bool
    timed_out: bool
    text: str


def _run_foreground(
    label: str,
    args: list[str],
    log_filename: str,
    timeout_seconds: int,
    env_extra: dict[str, str] | None = None,
) -> ForegroundRunResult:
    _ensure_dev_log_dir()
    log_path = DEV_LOG_DIR / log_filename
    run_env = os.environ.copy()
    if env_extra:
        run_env.update(env_extra)
    tout = None if timeout_seconds <= 0 else timeout_seconds
    with open(log_path, "w", encoding="utf-8") as logf:
        proc = subprocess.Popen(
            args,
            cwd=REPO_ROOT,
            env=run_env,
            stdout=logf,
            stderr=subprocess.STDOUT,
            stdin=subprocess.DEVNULL,
            start_new_session=True,
        )
        try:
            returncode = proc.wait(timeout=tout)
        except subprocess.TimeoutExpired:
            try:
                os.killpg(proc.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                try:
                    os.killpg(proc.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                proc.wait()
            tail = _tail_file(log_path, 80)
            return ForegroundRunResult(
                False,
                True,
                f"{label} timed out after {timeout_seconds}s. log: {log_path}\n"
                f"--- tail ---\n{tail}",
            )
    if returncode != 0:
        tail = _tail_file(log_path, 80)
        return ForegroundRunResult(
            False,
            False,
            f"{label} failed (exit {returncode}). log: {log_path}\n"
            f"--- tail ---\n{tail}",
        )
    tail = _tail_file(log_path, 40)
    return ForegroundRunResult(
        True,
        False,
        f"{label} succeeded (exit 0). log: {log_path}\n--- last lines ---\n{tail}",
    )


# ---------------------------------------------------------------------------
# MCP Tools
# ---------------------------------------------------------------------------

@mcp.tool()
def dashboard_serve(
    database_dsn: str | None = None,
    dashboard_port: int = 8180,
    proxy_port: int = 8443,
    restart: bool = False,
) -> str:
    """Start the dashboard server and mock-oauth-proxy in the background.

    Runs database migrations, generates an HMAC secret, starts the dashboard
    on ``dashboard_port`` (default 8180), and starts mock-oauth-proxy on
    ``proxy_port`` (default 8443). Skips starting if already running unless
    ``restart`` is True.
    """
    existing_dash = _pids_dashboard()
    existing_proxy = _pids_oauth_proxy()
    if existing_dash or existing_proxy:
        if not restart:
            pids = ", ".join(str(p) for p in existing_dash + existing_proxy)
            return (
                f"dashboard_serve already running (pid(s) {pids}). "
                f"Dashboard: http://localhost:{dashboard_port} "
                f"Proxy: http://localhost:{proxy_port} "
                f"Call with restart=True to restart."
            )
        if existing_dash:
            _stop_pids(existing_dash)
        if existing_proxy:
            _stop_pids(existing_proxy)
        _free_tcp_port(dashboard_port)
        _free_tcp_port(proxy_port)

    dsn = database_dsn or _default_dsn()
    log_path = DEV_LOG_DIR / "dashboard_serve.log"

    return _run_script_background(
        label="dashboard_serve",
        script="hack/local/dashboard/local-dev.sh",
        args=[dsn],
        log_path=log_path,
        ready_url=f"http://localhost:{dashboard_port}/health",
        env_extra={
            "DASHBOARD_PORT": str(dashboard_port),
            "PROXY_PORT": str(proxy_port),
        },
    )


FRONTEND_DEV_PORT = 3030


@mcp.tool()
def frontend_start(
    dashboard_port: int = 8180,
    proxy_port: int = 8443,
    frontend_port: int = FRONTEND_DEV_PORT,
    restart: bool = False,
) -> str:
    """Start the Vite dev server (``npm run start`` in ``frontend``) in the background.

    Defaults to port 3030. Sets ``VITE_PUBLIC_DOMAIN`` and ``VITE_PROTECTED_DOMAIN``
    based on the dashboard and proxy ports. Skips starting if already running unless
    ``restart`` is True.
    """
    frontend_dir = REPO_ROOT / "frontend"
    if not (frontend_dir / "package.json").is_file():
        return f"frontend not found or missing package.json: {frontend_dir}"

    existing = _pids_frontend()
    if existing:
        if not restart:
            pids = ", ".join(str(p) for p in existing)
            return (
                f"frontend_start already running (pid(s) {pids}). "
                f"URL: http://localhost:{frontend_port}. "
                f"Call with restart=True to restart."
            )
        _stop_pids(existing)
    if restart:
        _free_tcp_port(frontend_port)

    _ensure_dev_log_dir()
    log_path = DEV_LOG_DIR / "frontend_start.log"
    log_path.parent.mkdir(parents=True, exist_ok=True)

    env = os.environ.copy()
    env["VITE_PUBLIC_DOMAIN"] = f"http://localhost:{dashboard_port}"
    env["VITE_PROTECTED_DOMAIN"] = f"http://localhost:{proxy_port}"

    logf = open(log_path, "a", encoding="utf-8")
    try:
        proc = subprocess.Popen(
            ["npm", "run", "start", "--", "--port", str(frontend_port)],
            cwd=frontend_dir,
            env=env,
            stdout=logf,
            stderr=subprocess.STDOUT,
            stdin=subprocess.DEVNULL,
            start_new_session=True,
        )
    except OSError as e:
        logf.close()
        return f"frontend_start failed to start: {e}"
    logf.close()

    time.sleep(0.75)
    code = proc.poll()
    if code is not None:
        tail = _tail_file(log_path, 40)
        return f"frontend_start exited immediately (exit {code}). log: {log_path}\n--- tail ---\n{tail}"

    deadline = time.monotonic() + 120
    ready = False
    while time.monotonic() < deadline:
        try:
            urllib.request.urlopen(f"http://localhost:{frontend_port}", timeout=2)
            ready = True
            break
        except Exception:
            if proc.poll() is not None:
                tail = _tail_file(log_path, 40)
                code = proc.poll()
                return f"frontend_start exited (exit {code}). log: {log_path}\n--- tail ---\n{tail}"
            time.sleep(1)

    code = proc.poll()
    if code is not None:
        tail = _tail_file(log_path, 40)
        return f"frontend_start exited (exit {code}). log: {log_path}\n--- tail ---\n{tail}"
    if not ready:
        _terminate_process_group(proc)
        tail = _tail_file(log_path, 40)
        return (
            f"frontend_start readiness timed out. pid {proc.pid}. log: {log_path}\n"
            f"--- tail ---\n{tail}"
        )

    return (
        f"frontend_start started (pid {proc.pid}). URL: http://localhost:{frontend_port} "
        f"log: {log_path}"
    )


@mcp.tool()
def run_migrate(
    database_dsn: str | None = None,
    timeout_seconds: int = 120,
) -> str:
    """Run database migrations (``go run ./cmd/migrate``).

    Uses ``SHIP_STATUS_DSN`` environment variable if ``database_dsn`` is not provided.
    """
    dsn = database_dsn or _default_dsn()
    return _run_foreground(
        "run_migrate",
        ["go", "run", "./cmd/migrate", "--dsn", dsn],
        "run_migrate.log",
        timeout_seconds,
    ).text


@mcp.tool()
def component_monitor_start(
    dashboard_url: str = "http://localhost:8443",
    restart: bool = False,
) -> str:
    """Start the component-monitor stack: mock-monitored-component, Prometheus, and component-monitor.

    Requires Prometheus installed locally. The dashboard should be running
    first (use ``dashboard_serve``). Skips starting if already running unless ``restart`` is True.
    """
    existing_monitor = _pids_component_monitor()
    existing_mock = _pids_mock_component()
    existing_prom = _pids_prometheus()
    if existing_monitor or existing_mock or existing_prom:
        if not restart:
            pids = ", ".join(str(p) for p in existing_monitor + existing_mock + existing_prom)
            return (
                f"component_monitor already running (pid(s) {pids}). "
                f"Call with restart=True to restart."
            )
        if existing_monitor:
            _stop_pids(existing_monitor)
        if existing_mock:
            _stop_pids(existing_mock)
        if existing_prom:
            _stop_pids(existing_prom)
        for port in (8081, 9090):
            _free_tcp_port(port)

    log_path = DEV_LOG_DIR / "component_monitor.log"

    return _run_script_background(
        label="component_monitor_start",
        script="hack/local/component-monitor/local-dev.sh",
        args=["--native-prometheus"],
        log_path=log_path,
        env_extra={"DASHBOARD_URL": dashboard_url},
    )


@mcp.tool()
def run_tests(
    timeout_seconds: int = 600,
) -> str:
    """Run lint and unit tests (``make lint`` then ``make test``).

    Does NOT run e2e tests. Use ``make local-e2e`` separately for e2e.
    """
    lint_result = _run_foreground(
        "lint",
        ["make", "lint"],
        "run_lint.log",
        timeout_seconds,
    )
    if not lint_result.success:
        return f"Lint failed, skipping tests.\n{lint_result.text}"

    test_result = _run_foreground(
        "test",
        ["make", "test"],
        "run_test.log",
        timeout_seconds,
    )

    return f"=== Lint ===\n{lint_result.text}\n\n=== Tests ===\n{test_result.text}"


if __name__ == "__main__":
    mcp.run()
