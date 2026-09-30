# API Endpoints

This document lists all API endpoints available in the SHIP Status Dashboard.

For authentication details, see [cmd/dashboard/README.md](cmd/dashboard/README.md).

Write endpoints support delegated authorization via the `X-Acting-For` HTTP header. Trusted service accounts (configured in `trusted_delegators`) must provide this header to identify the user they are acting on behalf of; the auth middleware resolves the delegated identity before handlers run, so authorization and auditing use the delegated user transparently. Regular authenticated users do not need this header and are authorized directly.

## Endpoints

### Component Status

- **GET** `/api/status` - Get status of all components
  - **Public:** Yes

- **GET** `/api/status/{componentName}` - Get status of a specific component
  - **Public:** Yes

- **GET** `/api/status/{componentName}/{subComponentName}` - Get status of a specific sub-component
  - **Public:** Yes
  - Response includes an optional `suspected_outage` object (`{ outage_id, report_count, description, start_time }`) when an unconfirmed community-reported outage exists. Suspected outages are excluded from the `active_outages` list.

### Component Information

- **GET** `/api/components` - Get list of all configured components
  - **Public:** Yes

- **GET** `/api/components/{componentName}` - Get information for a specific component
  - **Public:** Yes

- **GET** `/api/sub-components` - List sub-components; optional query parameters `componentName`, `tag`, `team`, and `status`. Filters combine with AND across parameter names (`componentName`, `tag`, `team`, and `status`). Within `status`, multiple values are matched with OR: `status` may be repeated and/or comma-separated (e.g. `status=Down&status=Degraded` or `status=Down,Degraded`) and returns sub-components matching any listed status. Valid `status` values are `Healthy`, `Degraded`, `Down`, `CapacityExhausted`, and `Suspected` (`Partial` is component-level only and is rejected). Each returned item includes a `status` field with the sub-component's current status.
  - **Public:** Yes

### Team SLOs

- **GET** `/api/teams/slo-summary` - Home-page roll-up. One block per `team_slos` entry. Evaluations and compact active outages for components listed in that entry's `slo_components`. No workspace item lists.
  - **Public:** Yes

- **GET** `/api/teams/{team}/slo` - Team page SLO: evaluations, active outages for components listed in `team_slos[].slo_components`, and stored workspace items.
  - **Public:** Yes

- **PUT** `/api/teams/{team}/slo/items` - Create or replace one workspace item (`kind`, `schema_version`, `item_key`, `group_key`, `occurred_at`, `outcome`, `details`, `notes`).
  - **Public:** No (requires authentication and team SLO authorization)
  - Supports `X-Acting-For` header for delegated authorization

- **DELETE** `/api/teams/{team}/slo/items/{kind}/{itemKey}` - Delete one workspace item.
  - **Public:** No (requires authentication and team SLO authorization)
  - Supports `X-Acting-For` header for delegated authorization

- **PUT** `/api/teams/{team}/slo/items/{kind}/{itemKey}/links` - Attach a link (`url`, `link_type` of `jira`, `outage`, or `other`, optional `outage_id`).
  - **Public:** No (requires authentication and team SLO authorization)
  - Supports `X-Acting-For` header for delegated authorization

- **DELETE** `/api/teams/{team}/slo/items/{kind}/{itemKey}/links/{linkId}` - Delete one workspace link.
  - **Public:** No (requires authentication and team SLO authorization)
  - Supports `X-Acting-For` header for delegated authorization

### Tags

- **GET** `/api/tags` - Get the configured tag definitions
  - **Public:** Yes

### Outages

- **GET** `/api/components/{componentName}/outages` - Get all outages for a component
  - **Public:** Yes

- **GET** `/api/components/{componentName}/{subComponentName}/outages` - Get all outages for a sub-component
  - **Public:** Yes

- **GET** `/api/outages/during` - Get outages overlapping a time window or instant (query params: `start` and/or `end` as RFC3339 or RFC3339Nano — at least one required; optional `componentName`, `subComponentName`, `tag`, `team` — `componentName`, `tag`, and `team` use the same AND rules as **GET** `/api/sub-components`; `subComponentName` is only allowed when `componentName` is set and narrows to that sub-component)
  - **Public:** Yes

- **GET** `/api/components/{componentName}/{subComponentName}/outages/{outageId}` - Get a specific outage by ID
  - **Public:** Yes
  - Response includes `last_auditable_update` (RFC3339), maintained by a DB trigger to match `CreatedAt` of the newest audit log for the outage.

- **POST** `/api/components/{componentName}/{subComponentName}/outages` - Create a new outage
  - **Public:** No (requires authentication and component authorization)
  - Supports `X-Acting-For` header for delegated authorization

- **PATCH** `/api/components/{componentName}/{subComponentName}/outages/{outageId}` - Update an existing outage
  - **Public:** No (requires authentication and component authorization)
  - Supports `X-Acting-For` header for delegated authorization

- **DELETE** `/api/components/{componentName}/{subComponentName}/outages/{outageId}` - Delete an outage
  - **Public:** No (requires authentication and component authorization)
  - Supports `X-Acting-For` header for delegated authorization

- **POST** `/api/components/{componentName}/{subComponentName}/outages/report-suspected` - Submit a community suspected outage report
  - **Public:** No (requires authentication)
  - Response: `{ outage, report_count, created }` — `created` is true when a new suspected outage was opened, `report_count` is the total number of reports on the outage.

### Outage History

- **GET** `/api/components/{componentName}/{subComponentName}/outage-history` - Get historical outage data for a sub-component
  - **Public:** Yes

### Audit Logs

- **GET** `/api/components/{componentName}/{subComponentName}/outages/{outageId}/audit-logs` - Get audit logs for a specific outage
  - **Public:** Yes

### Triage Notes

- **GET** `/api/components/{componentName}/{subComponentName}/outages/{outageId}/triage-notes` - Get all triage notes for an outage
  - **Public:** Yes

- **POST** `/api/components/{componentName}/{subComponentName}/outages/{outageId}/triage-notes` - Add a triage note to an outage
  - **Public:** No (requires authentication and component authorization)
  - Supports `X-Acting-For` header for delegated authorization

- **PATCH** `/api/components/{componentName}/{subComponentName}/outages/{outageId}/triage-notes/{noteId}` - Update a triage note
  - **Public:** No (requires authentication and component authorization or note authorship)
  - Supports `X-Acting-For` header for delegated authorization

- **DELETE** `/api/components/{componentName}/{subComponentName}/outages/{outageId}/triage-notes/{noteId}` - Delete a triage note
  - **Public:** No (requires authentication and component authorization or note authorship)
  - Supports `X-Acting-For` header for delegated authorization

### Outage Links

- **GET** `/api/components/{componentName}/{subComponentName}/outages/{outageId}/links` - Get all links for an outage
  - **Public:** Yes

- **POST** `/api/components/{componentName}/{subComponentName}/outages/{outageId}/links` - Add a link to an outage
  - **Public:** No (requires authentication and component authorization)
  - Supports `X-Acting-For` header for delegated authorization

- **PATCH** `/api/components/{componentName}/{subComponentName}/outages/{outageId}/links/{linkId}` - Update an outage link
  - **Public:** No (requires authentication and component authorization)
  - Supports `X-Acting-For` header for delegated authorization

- **DELETE** `/api/components/{componentName}/{subComponentName}/outages/{outageId}/links/{linkId}` - Delete an outage link
  - **Public:** No (requires authentication and component authorization)
  - Supports `X-Acting-For` header for delegated authorization

### Outage Relationships

- **GET** `/api/components/{componentName}/{subComponentName}/outages/{outageId}/relationships` - List relationships for an outage
  - **Public:** Yes

- **POST** `/api/components/{componentName}/{subComponentName}/outages/{outageId}/relationships` - Create a relationship between two outages
  - **Public:** No (requires authentication and component authorization)
  - Supports `X-Acting-For` header for delegated authorization

- **DELETE** `/api/components/{componentName}/{subComponentName}/outages/{outageId}/relationships/{relationshipId}` - Delete an outage relationship
  - **Public:** No (requires authentication and component authorization)
  - Supports `X-Acting-For` header for delegated authorization

### External Pages

- **GET** `/api/external-pages/{pageSlug}` - Get an external page by slug
  - **Public:** Yes

### User Information

- **GET** `/api/user` - Get authenticated user information
  - **Public:** No (requires authentication)

### Component Monitor Reports

- **POST** `/api/component-monitor/report` - Submit component monitor status report
  - **Public:** No (requires service account authentication)
  - Each `reasons[]` entry may include `links`, an array of `{url, link_type}`. Valid `link_type` values are `incident_channel_thread`, `rca`, `jira`, and `other` (default if omitted). On outage create, these become outage links. They are not stored on the reason row.
