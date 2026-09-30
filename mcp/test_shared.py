"""Tests for SHIP Status API MCP client."""

from unittest.mock import MagicMock, patch

import pytest

from shared import DashboardClient, ShipStatusAPI, _outage_is_active


def _list_outage_no_slack(*, active: bool) -> dict:
    end = None if active else "2026-05-02T12:00:00Z"
    return {
        "ID": 6041,
        "CreatedAt": "2026-05-01T10:00:00Z",
        "component_name": "build-farm",
        "sub_component_name": "build06",
        "severity": "Degraded",
        "start_time": "2026-05-01T10:05:00Z",
        "end_time": end,
        "description": "Component monitor detected outage",
        "reasons": [{"type": "junit"}],
    }


def _detail_with_slack() -> dict:
    raw = _list_outage_no_slack(active=True)
    raw["slack_threads"] = [{"channel_id": "C01234567"}]
    return raw


@pytest.fixture
def client() -> DashboardClient:
    return DashboardClient(
        public_base_url="http://test/api",
        protected_base_url="http://test-protected/api",
        dashboard_url="http://test/",
    )


@pytest.fixture
def api(client: DashboardClient) -> ShipStatusAPI:
    return ShipStatusAPI(client)


def test_public_get_builds_url(client: DashboardClient):
    with patch("shared.urlopen") as mock_open:
        mock_resp = MagicMock()
        mock_resp.read.return_value = b'[{"ok": true}]'
        mock_resp.__enter__ = lambda s: s
        mock_resp.__exit__ = MagicMock(return_value=False)
        mock_open.return_value = mock_resp

        result = client.public_get("/status")
        assert result == [{"ok": True}]
        call_req = mock_open.call_args[0][0]
        assert call_req.full_url == "http://test/api/status"


def test_protected_request_without_token(client: DashboardClient):
    with patch.dict("os.environ", {}, clear=True):
        result = client.protected_request("POST", "/components/x/y/outages", body={})
    assert "error" in result
    assert "SHIP_STATUS_AUTH_TOKEN_FILE" in result["error"]


def test_protected_request_with_bearer(tmp_path):
    token_file = tmp_path / "token"
    token_file.write_text("sa-token-xyz", encoding="utf-8")
    client = DashboardClient(
        public_base_url="http://test/api",
        protected_base_url="http://test-protected/api",
        dashboard_url="http://test/",
        auth_token_file=str(token_file),
    )
    with patch.dict("os.environ", {}, clear=True):
        with patch("shared.urlopen") as mock_open:
            mock_resp = MagicMock()
            mock_resp.read.return_value = b'{"id": 1}'
            mock_resp.__enter__ = lambda s: s
            mock_resp.__exit__ = MagicMock(return_value=False)
            mock_open.return_value = mock_resp

            result = client.protected_request("POST", "/components/a/b/outages", body={"x": 1})
            assert result == {"id": 1}
            call_req = mock_open.call_args[0][0]
            assert call_req.get_header("Authorization") == "Bearer sa-token-xyz"


def test_list_components_returns_api_error_at_top_level(api: ShipStatusAPI):
    with patch.object(api.client, "public_get", return_value={"error": "HTTP 500: boom"}):
        result = api.list_components()
    assert result == {"error": "HTTP 500: boom"}


def test_list_sub_components_status_filter(api: ShipStatusAPI):
    with patch.object(api.client, "public_get", return_value=[{"name": "Deck", "status": "Down"}]) as mock_get:
        result = api.list_sub_components(status="Down,Degraded")
    assert result["sub_components"][0]["status"] == "Down"
    mock_get.assert_called_once_with("/sub-components?status=Down&status=Degraded")


def test_list_sub_components_malformed_status_filter(api: ShipStatusAPI):
    with patch.object(api.client, "public_get") as mock_get:
        result = api.list_sub_components(status=", ,")
    assert result == {"error": "status filter must include at least one status"}
    mock_get.assert_not_called()


def test_get_infrastructure_status(api: ShipStatusAPI):
    with patch.object(api.client, "public_get", return_value=[{"component_name": "Prow", "status": "Healthy", "active_outages": []}]):
        result = api.get_infrastructure_status()
    assert result["overall_healthy"] is True
    assert result["components"][0]["component_name"] == "Prow"


def test_get_component_outages_passthrough(api: ShipStatusAPI):
    with patch.object(api.client, "public_get", return_value=[_list_outage_no_slack(active=True)]):
        result = api.get_component_outages("build-farm", "build06")
    assert result["active_count"] == 1
    assert result["outages"][0]["ID"] == 6041


def test_get_component_outages_merges_slack(api: ShipStatusAPI):
    list_row = _list_outage_no_slack(active=True)

    def fake_get(path: str):
        if path.endswith("/outages"):
            return [list_row]
        if path.endswith("/outages/6041"):
            return _detail_with_slack()
        raise AssertionError(path)

    with patch.object(api.client, "public_get", side_effect=fake_get):
        result = api.get_component_outages("build-farm", "build06")
    assert result["outages"][0]["slack_threads"][0]["channel_id"] == "C01234567"


def test_get_outages_during_requires_start_or_end(api: ShipStatusAPI):
    result = api.get_outages_during()
    assert "error" in result


def test_get_outages_during_rejects_sub_without_component(api: ShipStatusAPI):
    result = api.get_outages_during(start="2026-05-13T00:00:00Z", sub_component_name="build04")
    assert "error" in result


def test_get_outages_during_builds_query(api: ShipStatusAPI):
    api_row = _list_outage_no_slack(active=False)

    def check_path(path: str):
        assert path.startswith("/outages/during?")
        assert "start=" in path
        assert "componentName=build-farm" in path
        return [api_row]

    with patch.object(api.client, "public_get", side_effect=check_path):
        result = api.get_outages_during(
            start="2026-05-13T00:00:00Z",
            end="2026-05-13T23:59:59Z",
            component_name="build-farm",
        )
    assert result["count"] == 1


def test_outage_is_active_sql_null():
    assert _outage_is_active({"end_time": {"Time": "2026-05-02T00:00:00Z", "Valid": False}})


# --- Write tool tests ---


def _authed_api(tmp_path) -> ShipStatusAPI:
    token_file = tmp_path / "token"
    token_file.write_text("sa-token-xyz", encoding="utf-8")
    client = DashboardClient(
        public_base_url="http://test/api",
        protected_base_url="http://test-protected/api",
        dashboard_url="http://test/",
        auth_token_file=str(token_file),
    )
    return ShipStatusAPI(client)


def test_create_outage_success(tmp_path):
    api = _authed_api(tmp_path)
    response = {"ID": 1, "severity": "Down", "description": "test"}
    with patch.object(api.client, "public_get", return_value=[]):
        with patch.object(api.client, "protected_request", return_value=response) as mock:
            result = api.create_outage("prow", "tide", severity="Down", description="test")
    assert result["ID"] == 1
    call_body = mock.call_args.kwargs["body"]
    assert call_body["severity"] == "Down"
    assert call_body["description"] == "test"
    assert call_body["discovered_from"] == "mcp"
    assert call_body["confirmed"] is True
    assert "start_time" in call_body
    assert "acting_for" not in call_body
    assert mock.call_args.kwargs["acting_for"] == ""


def test_create_outage_bot_initiated_forces_degraded(tmp_path):
    api = _authed_api(tmp_path)
    response = {"ID": 3, "severity": "Degraded"}
    with patch.object(api.client, "public_get", return_value=[]):
        with patch.object(api.client, "protected_request", return_value=response) as mock:
            result = api.create_outage(
                "prow", "tide", severity="Down", description="bot detected",
                bot_initiated=True, acting_for="chai-bot",
            )
    assert result["ID"] == 3
    body = mock.call_args.kwargs["body"]
    assert body["severity"] == "Degraded"
    assert body["confirmed"] is False
    assert body["discovered_from"] == "mcp"
    assert "acting_for" not in body
    assert mock.call_args.kwargs["acting_for"] == "chai-bot"


def test_create_outage_bot_initiated_returns_existing(tmp_path):
    api = _authed_api(tmp_path)
    active_outage = {"ID": 99, "severity": "Down", "end_time": None}
    with patch.object(api.client, "public_get", return_value=[active_outage]):
        with patch.object(api.client, "protected_request") as mock_protected:
            result = api.create_outage(
                "prow", "tide", severity="Down", description="bot detected", bot_initiated=True
            )
    assert result["existing_outage"] is True
    assert result["outage"]["ID"] == 99
    mock_protected.assert_not_called()


def test_create_outage_user_initiated_returns_existing(tmp_path):
    api = _authed_api(tmp_path)
    active_outage = {"ID": 99, "severity": "Down", "end_time": None}
    with patch.object(api.client, "public_get", return_value=[active_outage]):
        with patch.object(api.client, "protected_request") as mock_protected:
            result = api.create_outage(
                "prow", "tide", severity="Down", description="user reported", bot_initiated=False
            )
    assert result["existing_outage"] is True
    assert result["outage"]["ID"] == 99
    mock_protected.assert_not_called()


def test_update_outage_resolve(tmp_path):
    api = _authed_api(tmp_path)
    response = {"ID": 1, "end_time": {"Time": "2026-06-29T14:00:00Z", "Valid": True}}
    with patch.object(api.client, "protected_request", return_value=response) as mock:
        result = api.update_outage("prow", "tide", 1, end_time="2026-06-29T14:00:00Z")
    assert result["ID"] == 1
    body = mock.call_args.kwargs["body"]
    assert body["end_time"] == {"Time": "2026-06-29T14:00:00Z", "Valid": True}


def test_update_outage_includes_acting_for(tmp_path):
    api = _authed_api(tmp_path)
    response = {"ID": 1, "severity": "Down"}
    with patch.object(api.client, "protected_request", return_value=response) as mock:
        result = api.update_outage("prow", "tide", 1, severity="Down", acting_for="jdoe")
    assert result["ID"] == 1
    body = mock.call_args.kwargs["body"]
    assert "acting_for" not in body
    assert body["severity"] == "Down"
    assert mock.call_args.kwargs["acting_for"] == "jdoe"


def test_update_outage_no_fields(tmp_path):
    api = _authed_api(tmp_path)
    result = api.update_outage("prow", "tide", 1)
    assert "error" in result
    assert "No fields to update" in result["error"]


def test_get_team_slo_quotes_team_and_returns_object(api: ShipStatusAPI):
    payload = {"team": "A B/C", "items": []}
    with patch.object(api.client, "public_get", return_value=payload) as mock_get:
        result = api.get_team_slo("A B/C")
    assert result == payload
    mock_get.assert_called_once_with("/teams/A%20B%2FC/slo")


def test_get_team_slo_returns_api_error(api: ShipStatusAPI):
    with patch.object(api.client, "public_get", return_value={"error": "HTTP 500: boom"}):
        result = api.get_team_slo("TRT")
    assert result == {"error": "HTTP 500: boom"}


def test_get_team_slo_rejects_non_object(api: ShipStatusAPI):
    with patch.object(api.client, "public_get", return_value=[{"team": "TRT"}]):
        result = api.get_team_slo("TRT")
    assert "error" in result


def test_get_team_slo_summary(api: ShipStatusAPI):
    payload = {"teams": [{"team": "TRT"}]}
    with patch.object(api.client, "public_get", return_value=payload) as mock_get:
        result = api.get_team_slo_summary()
    assert result == payload
    mock_get.assert_called_once_with("/teams/slo-summary")


def test_get_team_slo_summary_failure(api: ShipStatusAPI):
    with patch.object(api.client, "public_get", return_value=None):
        result = api.get_team_slo_summary()
    assert result == {"error": "Failed to retrieve SLO summary."}


def test_get_team_slo_summary_rejects_non_object(api: ShipStatusAPI):
    with patch.object(api.client, "public_get", return_value=["TRT"]):
        result = api.get_team_slo_summary()
    assert "error" in result


def test_upsert_slo_item_puts_body_and_acting_for(api: ShipStatusAPI):
    with patch.object(api.client, "protected_request", return_value={"id": 7}) as mock:
        result = api.upsert_slo_item(
            "A B/C",
            "payload/streams",
            1,
            "item/key",
            "nightly",
            "2026-09-25T12:00:00Z",
            "Accepted",
            {"payload_url": "https://example.com"},
            notes="kept",
            acting_for="chai-bot",
        )
    assert result["id"] == 7
    assert mock.call_args.args[0] == "PUT"
    assert mock.call_args.args[1] == "/teams/A%20B%2FC/slo/items"
    body = mock.call_args.kwargs["body"]
    assert body["kind"] == "payload/streams"
    assert body["schema_version"] == 1
    assert body["item_key"] == "item/key"
    assert body["group_key"] == "nightly"
    assert body["occurred_at"] == "2026-09-25T12:00:00Z"
    assert body["outcome"] == "Accepted"
    assert body["details"] == {"payload_url": "https://example.com"}
    assert body["notes"] == "kept"
    assert "acting_for" not in body
    assert mock.call_args.kwargs["acting_for"] == "chai-bot"


def test_upsert_slo_item_returns_api_error(api: ShipStatusAPI):
    with patch.object(api.client, "protected_request", return_value={"error": "HTTP 403: no"}):
        result = api.upsert_slo_item(
            "TRT", "payload_streams", 1, "k", "g", "2026-09-25T12:00:00Z", "Rejected", {}, acting_for="x"
        )
    assert result == {"error": "HTTP 403: no"}


def test_upsert_slo_item_rejects_non_object(api: ShipStatusAPI):
    with patch.object(api.client, "protected_request", return_value=[{"id": 1}]):
        result = api.upsert_slo_item("TRT", "payload_streams", 1, "k", "g", "2026-09-25T12:00:00Z", "Rejected", {})
    assert "error" in result


def test_add_slo_item_link_optional_outage_and_acting_for(api: ShipStatusAPI):
    with patch.object(api.client, "protected_request", return_value={"ID": 4}) as mock:
        result = api.add_slo_item_link(
            "TRT",
            "payload/streams",
            "a/b",
            "https://example.com/j",
            link_type="jira",
            outage_id=12,
            acting_for="chai-bot",
        )
    assert result["ID"] == 4
    assert mock.call_args.args[0] == "PUT"
    assert mock.call_args.args[1] == "/teams/TRT/slo/items/payload%2Fstreams/a%2Fb/links"
    body = mock.call_args.kwargs["body"]
    assert body == {"url": "https://example.com/j", "link_type": "jira", "outage_id": 12}
    assert "acting_for" not in body
    assert mock.call_args.kwargs["acting_for"] == "chai-bot"


def test_add_slo_item_link_omits_outage_id(api: ShipStatusAPI):
    with patch.object(api.client, "protected_request", return_value={"ID": 5}) as mock:
        api.add_slo_item_link("TRT", "payload_streams", "k", "https://example.com", link_type="other")
    assert "outage_id" not in mock.call_args.kwargs["body"]
    assert mock.call_args.kwargs["acting_for"] == ""


def test_delete_outage_success(tmp_path):
    api = _authed_api(tmp_path)
    with patch.object(api.client, "protected_request", return_value=None):
        result = api.delete_outage("prow", "tide", 1)
    assert result["success"] is True


