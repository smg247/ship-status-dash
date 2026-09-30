"""Tests for MCP server tool registration (public and auth servers)."""

import asyncio

from public_server import build_server as build_public_server
from auth_server import build_server as build_auth_server


_READ_TOOLS = {
    "get_infrastructure_status",
    "get_component_details",
    "get_component_outages",
    "get_outage",
    "get_outages_during",
    "list_components",
    "list_tags",
    "list_sub_components",
    "get_team_slo",
    "get_team_slo_summary",
}

_WRITE_TOOLS = {
    "create_outage",
    "update_outage",
    "delete_outage",
    "add_triage_note",
    "update_triage_note",
    "delete_triage_note",
    "add_outage_link",
    "update_outage_link",
    "delete_outage_link",
    "add_outage_relationship",
    "delete_outage_relationship",
    "upsert_slo_item",
    "add_slo_item_link",
}


def _tool_names(server) -> set[str]:
    tools = asyncio.run(server.list_tools())
    return {t.name for t in tools}


def _tools_by_name(server) -> dict[str, object]:
    tools = asyncio.run(server.list_tools())
    return {t.name: t for t in tools}


def test_public_server_only_registers_read_tools():
    server = build_public_server()
    assert _tool_names(server) == _READ_TOOLS


def test_auth_server_only_registers_write_tools():
    server = build_auth_server()
    assert _tool_names(server) == _WRITE_TOOLS


def test_no_overlap_between_servers():
    pub = _tool_names(build_public_server())
    auth = _tool_names(build_auth_server())
    assert pub & auth == set()


def test_write_tools_have_acting_for_parameter():
    """All write tools accept acting_for."""
    server = build_auth_server()
    tools = _tools_by_name(server)
    for name in _WRITE_TOOLS:
        param_names = set(tools[name].parameters.get("properties", {}).keys())
        assert "acting_for" in param_names, f"{name} missing acting_for parameter"
