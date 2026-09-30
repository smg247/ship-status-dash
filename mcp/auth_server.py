"""Authenticated (write) MCP server for the SHIP Status Dashboard.

Exposes write tools that call the protected API. Requires a Bearer token
(SHIP_STATUS_AUTH_TOKEN_FILE) and acting_for to identify the delegated user.
Deployed behind oauth-proxy.
"""

from __future__ import annotations

import logging
import os

from fastmcp import FastMCP

from shared import ShipStatusAPI

logger = logging.getLogger(__name__)

_DEFAULT_MCP_HTTP_PORT = 8091


def _register_write_tools(server: FastMCP, api: ShipStatusAPI) -> None:
    @server.tool()
    def create_outage(
        component_slug: str,
        sub_component_slug: str,
        severity: str,
        description: str,
        acting_for: str = "",
        start_time: str = "",
        initial_triage_note: str = "",
        bot_initiated: bool = False,
    ) -> dict:
        """Create an outage on a sub-component. acting_for identifies the user/bot responsible (required in authenticated mode). Severity: Down, Degraded, Suspected, or CapacityExhausted. start_time is RFC3339 UTC (defaults to now). Set bot_initiated=true when creating autonomously (forces Degraded severity, unconfirmed, and checks for duplicates)."""
        return api.create_outage(
            component_slug,
            sub_component_slug,
            severity=severity,
            description=description,
            start_time=start_time,
            initial_triage_note=initial_triage_note,
            bot_initiated=bot_initiated,
            acting_for=acting_for,
        )

    @server.tool()
    def update_outage(
        component_slug: str,
        sub_component_slug: str,
        outage_id: int,
        acting_for: str = "",
        severity: str = "",
        description: str = "",
        start_time: str = "",
        end_time: str = "",
        confirmed: bool | None = None,
    ) -> dict:
        """Update an existing outage. acting_for identifies the user/bot responsible (required in authenticated mode). Set end_time (RFC3339 UTC) to resolve it. All other fields are optional — only provided fields are changed."""
        return api.update_outage(
            component_slug,
            sub_component_slug,
            outage_id,
            severity=severity,
            description=description,
            start_time=start_time,
            end_time=end_time,
            confirmed=confirmed,
            acting_for=acting_for,
        )

    @server.tool()
    def delete_outage(
        component_slug: str, sub_component_slug: str, outage_id: int, acting_for: str = ""
    ) -> dict:
        """Permanently delete an outage. acting_for identifies the user/bot responsible (required in authenticated mode). Prefer resolving (update_outage with end_time) over deleting."""
        return api.delete_outage(component_slug, sub_component_slug, outage_id, acting_for=acting_for)

    @server.tool()
    def add_triage_note(
        component_slug: str, sub_component_slug: str, outage_id: int, body: str, acting_for: str = ""
    ) -> dict:
        """Add a triage note to an outage for incident documentation. acting_for identifies the user/bot responsible (required in authenticated mode)."""
        return api.add_triage_note(
            component_slug, sub_component_slug, outage_id, body, acting_for=acting_for
        )

    @server.tool()
    def update_triage_note(
        component_slug: str,
        sub_component_slug: str,
        outage_id: int,
        note_id: int,
        body: str,
        acting_for: str = "",
    ) -> dict:
        """Update an existing triage note. acting_for identifies the user/bot responsible (required in authenticated mode)."""
        return api.update_triage_note(
            component_slug, sub_component_slug, outage_id, note_id, body, acting_for=acting_for
        )

    @server.tool()
    def delete_triage_note(
        component_slug: str,
        sub_component_slug: str,
        outage_id: int,
        note_id: int,
        acting_for: str = "",
    ) -> dict:
        """Delete a triage note from an outage. acting_for identifies the user/bot responsible (required in authenticated mode)."""
        return api.delete_triage_note(
            component_slug, sub_component_slug, outage_id, note_id, acting_for=acting_for
        )

    @server.tool()
    def add_outage_link(
        component_slug: str,
        sub_component_slug: str,
        outage_id: int,
        url: str,
        acting_for: str = "",
        link_type: str = "other",
        description: str = "",
    ) -> dict:
        """Attach a link to an outage. acting_for identifies the user/bot responsible (required in authenticated mode). link_type: incident_channel_thread, rca, jira, or other."""
        return api.add_outage_link(
            component_slug, sub_component_slug, outage_id, url,
            link_type=link_type, description=description, acting_for=acting_for,
        )

    @server.tool()
    def update_outage_link(
        component_slug: str,
        sub_component_slug: str,
        outage_id: int,
        link_id: int,
        url: str,
        acting_for: str = "",
        link_type: str = "other",
        description: str = "",
    ) -> dict:
        """Update an existing outage link. acting_for identifies the user/bot responsible (required in authenticated mode)."""
        return api.update_outage_link(
            component_slug, sub_component_slug, outage_id, link_id, url,
            link_type=link_type, description=description, acting_for=acting_for,
        )

    @server.tool()
    def delete_outage_link(
        component_slug: str,
        sub_component_slug: str,
        outage_id: int,
        link_id: int,
        acting_for: str = "",
    ) -> dict:
        """Delete a link from an outage. acting_for identifies the user/bot responsible (required in authenticated mode)."""
        return api.delete_outage_link(
            component_slug, sub_component_slug, outage_id, link_id, acting_for=acting_for
        )


    @server.tool()
    def add_outage_relationship(
        component_slug: str,
        sub_component_slug: str,
        outage_id: int,
        related_outage_id: int,
        acting_for: str = "",
        relationship_type: str = "related_to",
    ) -> dict:
        """Link two outages with a relationship. acting_for identifies the user/bot responsible (required in authenticated mode). relationship_type: causes, caused_by, or related_to."""
        return api.add_outage_relationship(
            component_slug, sub_component_slug, outage_id, related_outage_id,
            relationship_type=relationship_type, acting_for=acting_for,
        )

    @server.tool()
    def delete_outage_relationship(
        component_slug: str,
        sub_component_slug: str,
        outage_id: int,
        relationship_id: int,
        acting_for: str = "",
    ) -> dict:
        """Delete a relationship between two outages. acting_for identifies the user/bot responsible (required in authenticated mode)."""
        return api.delete_outage_relationship(
            component_slug, sub_component_slug, outage_id, relationship_id, acting_for=acting_for,
        )

    @server.tool()
    def upsert_slo_item(
        team: str,
        kind: str,
        schema_version: int,
        item_key: str,
        group_key: str,
        occurred_at: str,
        outcome: str,
        details: dict,
        acting_for: str = "",
        notes: str = "",
    ) -> dict:
        """Create or replace one SLO workspace item. acting_for is required. schema_version must match a schema this dashboard accepts. Does not create an outage."""
        return api.upsert_slo_item(
            team,
            kind,
            schema_version,
            item_key,
            group_key,
            occurred_at,
            outcome,
            details,
            notes=notes,
            acting_for=acting_for,
        )

    @server.tool()
    def add_slo_item_link(
        team: str,
        kind: str,
        item_key: str,
        url: str,
        acting_for: str = "",
        link_type: str = "other",
        outage_id: int | None = None,
    ) -> dict:
        """Attach a jira, outage, or other link to an SLO workspace item. acting_for is required. Does not create an outage."""
        return api.add_slo_item_link(
            team,
            kind,
            item_key,
            url,
            link_type=link_type,
            outage_id=outage_id,
            acting_for=acting_for,
        )


def build_server() -> FastMCP:
    """Build the authenticated (write) MCP server."""
    server = FastMCP("ship-status-auth")
    api = ShipStatusAPI()
    _register_write_tools(server, api)
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
