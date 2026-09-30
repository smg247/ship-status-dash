# Dashboard Component

The dashboard is a web application for viewing and managing component status, availability, and outages. It consists of:
- Backend: Go server (`cmd/dashboard`)
- Frontend: React application (`frontend/`)

## Production Deployment

### Authentication Architecture

The production deployment uses a dual ingress architecture with a single backing service:

- **Public Route**: `ship-status.ci.openshift.org` → Service port `8080` → Dashboard container
- **Protected Route**: `protected.ship-status.ci.openshift.org` → Service port `8443` → OAuth proxy container

Both routes point to the same Kubernetes Service (`dashboard`), which exposes two ports:
- Port `8080`: Direct access to the dashboard container (public routes, no authentication)
- Port `8443`: Access through the oauth-proxy container (protected routes, requires authentication)

### Pod Architecture

Each deployment pod contains two containers:

1. **Dashboard Container** (port 8080)
   - Serves public API endpoints (read-only status endpoints)
   - Validates HMAC signatures for protected endpoints
   - Expects `X-Forwarded-User` header and `GAP-Signature` header from oauth-proxy

2. **OAuth Proxy Container** (port 8443)
   - Handles OpenShift OAuth authentication
   - Proxies authenticated requests to `localhost:8080` (dashboard container)
   - Adds authentication headers:
     - `X-Forwarded-User`: Authenticated username
     - `X-Forwarded-Access-Token`: OAuth access token
     - Other headers that we don't currently care about
   - Signs requests with HMAC using shared secret
   - Adds `GAP-Signature` header for request verification

### Authentication Flow

**Public Routes** (no authentication):
```
Client → Ingress (ship-status.ci.openshift.org) → Service:8080 → Dashboard Container
```

**Protected Routes** (authentication required):
```
Client → Ingress (protected.ship-status.ci.openshift.org) → Service:8443 → OAuth Proxy
  → Dashboard Container (localhost:8080)
```

The dashboard container validates protected requests by:
1. Checking for `X-Forwarded-User` header
2. Verifying the `GAP-Signature` HMAC signature using the shared secret
3. Extracting user identity from headers for authorization checks

### HMAC Signature

Both oauth-proxy and dashboard share the same HMAC secret. The signature includes:
- Content-Length, Content-MD5, Content-Type
- Date
- Authorization
- X-Forwarded-User, X-Forwarded-Email, X-Forwarded-Access-Token
- Cookie, Gap-Auth

Each of these headers are included when the OpenShift Oauth Proxy creates it's signature, and we must provide complete parity.
See [SignatureHeaders](https://github.com/openshift/oauth-proxy/blob/master/oauthproxy.go).

## MCP Servers (`ship-status`)

The MCP servers ([`mcp/`](../../mcp/)) expose dashboard REST API tools for AI agents. They run as sidecar containers in the dashboard pod. Each server is a separate entry point:

- **Public MCP** (`public_server.py`, port 8090): Read-only tools for querying status, outages, component discovery, and team SLOs (`get_team_slo`, `get_team_slo_summary`). No authentication required. Route: `mcp.ship-status.ci.openshift.org`.
- **Authenticated MCP** (`auth_server.py`, port 8091): Write tools for outages, triage notes, links, and SLO workspace items (`upsert_slo_item`, `add_slo_item_link`). Requires authentication via oauth-proxy. Route: `protected-mcp.ship-status.ci.openshift.org`. Calls the dashboard's protected API using a mounted service account token (`SHIP_STATUS_AUTH_TOKEN_FILE`) and passes `X-Acting-For` header for delegated authorization. Team SLO writes still require the acting user to be an owner on that team's `team_slos` entry.

OpenShift Deployment/Route changes are maintained in [openshift/release](https://github.com/openshift/release).

Local dev MCP for starting the stack is **`ship-status-dev`** ([`ship-status-dev/`](../../ship-status-dev/)), not these servers.

## Open Graph Metadata

The SPA handler injects Open Graph and standard HTML metadata into the `index.html` response for each route, enabling rich link previews in Slack and other clients that read OG tags. When adding a new frontend route, add a corresponding pattern to `metaRoutes` in `meta.go` so link previews render correctly.

## Slack Integration

The dashboard supports Slack integration for outage reporting. When enabled, the dashboard will:

- Post outage notifications to configured Slack channels when outages are created or resolved
- Create threaded conversations for outage updates
- Include links back to the dashboard for viewing outage details

### Configuration

Slack integration is enabled by setting the `SLACK_BOT_TOKEN` environment variable with a valid Slack bot token. The dashboard also requires the `--slack-base-url` flag to be set, which is used to construct links in Slack messages.
