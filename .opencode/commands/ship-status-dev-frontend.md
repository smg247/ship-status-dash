---
description: Start the Vite React dev server via the ship-status-dev MCP tool
---

# Ship Status dev — frontend

Use the **`frontend_start`** MCP tool (server: **`ship-status-dev`**). Do not run `npm run start` in `frontend` manually — the MCP tool handles environment variables, background process management, log routing, and duplicate detection.

Typical URL: **`http://localhost:3030`**. The tool sets `VITE_PUBLIC_DOMAIN` to the dashboard and `VITE_PROTECTED_DOMAIN` to that Vite origin. Vite proxies `/oauth` and `/api` to the mock oauth proxy.

If the dev server is already running, the tool will report it. Ask the user if they want to restart, and if so call again with **`restart=True`**.

Log: **`ship-status-dev-logs/frontend_start.log`**. See **`ship-status-dev/server.py`** for all parameters.