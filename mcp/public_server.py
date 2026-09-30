"""Public (read-only) MCP server for the SHIP Status Dashboard.

Exposes read tools that query the public API. No credentials required.
"""

from __future__ import annotations

import logging
import os

from fastmcp import FastMCP

from shared import ShipStatusAPI

logger = logging.getLogger(__name__)

_DEFAULT_MCP_HTTP_PORT = 8090


def _register_read_tools(server: FastMCP, api: ShipStatusAPI) -> None:
    @server.tool()
    def get_infrastructure_status() -> dict:
        """Current health of all SHIP infrastructure components and active outages."""
        return api.get_infrastructure_status()

    @server.tool()
    def get_component_details(component_slug: str) -> dict:
        """Component config: description, team, and sub-components. Slugs are lowercase with hyphens (e.g. prow, build-farm)."""
        return api.get_component_details(component_slug)

    @server.tool()
    def get_component_outages(component_slug: str, sub_component_slug: str = "") -> dict:
        """Outage history for a component or sub-component (active and resolved). Omit sub_component_slug for all subs."""
        return api.get_component_outages(component_slug, sub_component_slug)

    @server.tool()
    def get_outage(component_slug: str, sub_component_slug: str, outage_id: int) -> dict:
        """Single outage by ID (includes slack_threads when present)."""
        return api.get_outage(component_slug, sub_component_slug, outage_id)

    @server.tool()
    def get_outages_during(
        start: str = "",
        end: str = "",
        component_name: str = "",
        sub_component_name: str = "",
        tag: str = "",
        team: str = "",
    ) -> dict:
        """Outages overlapping a time instant or interval (RFC3339 UTC). At least one of start or end is required."""
        return api.get_outages_during(
            start=start,
            end=end,
            component_name=component_name,
            sub_component_name=sub_component_name,
            tag=tag,
            team=team,
        )

    @server.tool()
    def list_components() -> dict:
        """All configured components (use slugs in other tools)."""
        return api.list_components()

    @server.tool()
    def list_tags() -> dict:
        """All dashboard tags (for filtering get_outages_during)."""
        return api.list_tags()

    @server.tool()
    def list_sub_components(status: str = "") -> dict:
        """All sub-components across components. Optional status filter: comma-separated values (Healthy, Degraded, Down, CapacityExhausted, Suspected). Partial is not valid for this filter. When set, only matching items are returned. Each item includes a status field."""
        return api.list_sub_components(status=status)

    @server.tool()
    def get_team_slo(team: str) -> dict:
        """Team SLO evaluations, watched streams, schema version, stored item keys, and recent workspace items."""
        return api.get_team_slo(team)

    @server.tool()
    def get_team_slo_summary() -> dict:
        """Per-team SLO roll-up and compact slo_component outages for the home page. No workspace item lists."""
        return api.get_team_slo_summary()


def build_server() -> FastMCP:
    """Build the public (read-only) MCP server."""
    server = FastMCP("ship-status")
    api = ShipStatusAPI()
    _register_read_tools(server, api)
    return server


def main() -> None:
    server = build_server()
    transport = os.environ.get("MCP_TRANSPORT", "stdio").strip() or "stdio"
    if transport == "stdio":
        server.run()
    else:
        port = _DEFAULT_MCP_HTTP_PORT
        raw = os.environ.get("MCP_HTTP_PORT", "").strip()
        if raw:
            try:
                parsed = int(raw)
                if 1 <= parsed <= 65535:
                    port = parsed
                else:
                    logger.warning(
                        "MCP_HTTP_PORT %r out of range (1-65535); using default %d",
                        raw,
                        port,
                    )
            except ValueError:
                logger.warning("Invalid MCP_HTTP_PORT %r; using default %d", raw, port)
        server.run(transport=transport, host="0.0.0.0", port=port)


if __name__ == "__main__":
    main()
