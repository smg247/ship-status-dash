# Ship Status Dashboard

SHIP Status and Availability Dashboard monitor

For architecture overview, production authentication, and dataflow diagrams, see [ARCHITECTURE.md](ARCHITECTURE.md).

## Project Structure

This project consists of multiple components:

- **Dashboard**: Web application for viewing and managing component status, availability, and outages
  - Backend: Go server (`cmd/dashboard`)
  - Frontend: React application (`frontend/`)
- **Component Monitor**: Monitoring service that periodically probes components and reports their status to the dashboard
  - Go service (`cmd/component-monitor`)
  - Supports HTTP, Prometheus, JUnit (Prow GCS canary), systemd, and Jira monitoring; see [`cmd/component-monitor/README.md`](cmd/component-monitor/README.md)

For local development setup, see [DEVELOPMENT.md](DEVELOPMENT.md).

## Dashboard Component

The dashboard is a web application for viewing and managing component status, availability, and outages. It consists of:

- Backend: Go server (`cmd/dashboard`)
- Frontend: React application (`frontend/`)

Frontend views include a team page at `/team/:team`, which lists sub-components owned by that SHIP team. The `:team` path segment is the URL-encoded team name (for example `/team/TRT`).

For detailed documentation, see [`cmd/dashboard/README.md`](cmd/dashboard/README.md).

## Component Monitor

The component-monitor is a service that periodically probes sub-components to detect outages and report their status to the dashboard API.

For detailed documentation, see [`cmd/component-monitor/README.md`](cmd/component-monitor/README.md).

## Configuration

The dashboard reads component definitions and settings from a YAML config file (synced from openshift/release via git-sync in production). This includes component owners, monitoring config, and `trusted_delegators` for delegated write authorization. Set `exclude_from_main_outage_well: true` on a sub-component to keep it out of the home-page In Outage well and the ship-logo fire indicator. `team_slos` defines per-team SLO objectives. See [API_ENDPOINTS.md](API_ENDPOINTS.md) for endpoint details.
