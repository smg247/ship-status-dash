"""HTTP client for the SHIP Status Dashboard public and protected APIs."""

from __future__ import annotations

import json
import logging
import os
from datetime import datetime, timezone
from pathlib import Path
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.parse import quote, urlencode
from urllib.request import Request, urlopen

logger = logging.getLogger(__name__)

DEFAULT_PUBLIC_API_URL = "https://ship-status.ci.openshift.org/api"
DEFAULT_PROTECTED_API_URL = "https://protected.ship-status.ci.openshift.org/api"
DEFAULT_DASHBOARD_URL = "https://ship-status.ci.openshift.org/"
DEFAULT_TIMEOUT = 10

_MAX_OUTAGES_SLACK_ENRICH = 60
_MAX_RESPONSE_CHARS = 28000


def _env(name: str, default: str) -> str:
    val = os.environ.get(name, "").strip()
    return val if val else default


def _read_bearer_token_file(path: str) -> str | None:
    try:
        return Path(path).read_text(encoding="utf-8").strip()
    except OSError as e:
        logger.error("Failed to read auth token file %s: %s", path, e)
        return None


class DashboardClient:
    """Wraps dashboard REST calls. Reads use the public API; writes use protected + Bearer SA."""

    def __init__(
        self,
        *,
        public_base_url: str | None = None,
        protected_base_url: str | None = None,
        dashboard_url: str | None = None,
        timeout: float | None = None,
        auth_token_file: str | None = None,
    ) -> None:
        legacy = os.environ.get("SHIP_STATUS_API_BASE_URL", "").strip()
        public_default = legacy or DEFAULT_PUBLIC_API_URL
        self.public_base_url = (public_base_url or _env("SHIP_STATUS_PUBLIC_API_URL", public_default)).rstrip(
            "/"
        )
        self.protected_base_url = (
            protected_base_url or _env("SHIP_STATUS_PROTECTED_API_URL", DEFAULT_PROTECTED_API_URL)
        ).rstrip("/")
        self.dashboard_url = (dashboard_url or _env("SHIP_STATUS_DASHBOARD_URL", DEFAULT_DASHBOARD_URL)).rstrip(
            "/"
        ) + "/"
        self.timeout = timeout if timeout is not None else float(
            os.environ.get("SHIP_STATUS_REQUEST_TIMEOUT", DEFAULT_TIMEOUT)
        )
        self.auth_token_file = auth_token_file.strip() if auth_token_file else None

    def _auth_token_path(self) -> str | None:
        if self.auth_token_file:
            return self.auth_token_file
        env_path = os.environ.get("SHIP_STATUS_AUTH_TOKEN_FILE", "").strip()
        return env_path or None

    def _load_bearer_token(self) -> str | None:
        path = self._auth_token_path()
        if not path:
            return None
        return _read_bearer_token_file(path)

    @property
    def writes_enabled(self) -> bool:
        return bool(self._load_bearer_token())

    def public_get(self, path: str) -> dict | list | None:
        return self._request("GET", self.public_base_url, path)

    def protected_request(
        self,
        method: str,
        path: str,
        body: dict | None = None,
        acting_for: str = "",
    ) -> dict | list | None:
        token = self._load_bearer_token()
        if not token:
            return {
                "error": (
                    "Protected API calls require SHIP_STATUS_AUTH_TOKEN_FILE "
                    "(OpenShift service account Bearer token)."
                )
            }
        headers = {"Authorization": f"Bearer {token}"}
        if acting_for:
            headers["X-Acting-For"] = acting_for
        data = None
        if body is not None:
            headers["Content-Type"] = "application/json"
            data = json.dumps(body).encode("utf-8")
        return self._request(method, self.protected_base_url, path, headers=headers, data=data)

    def _request(
        self,
        method: str,
        base_url: str,
        path: str,
        *,
        headers: dict[str, str] | None = None,
        data: bytes | None = None,
    ) -> dict | list | None:
        if not path.startswith("/"):
            path = "/" + path
        url = f"{base_url}{path}"
        req_headers = dict(headers or {})
        try:
            req = Request(url, data=data, headers=req_headers, method=method)
            with urlopen(req, timeout=self.timeout) as response:
                raw = response.read().decode()
                if not raw:
                    return None
                return json.loads(raw)
        except HTTPError as e:
            try:
                body = e.read().decode()
                parsed = json.loads(body)
                if isinstance(parsed, dict):
                    return parsed
            except (json.JSONDecodeError, OSError):
                pass
            logger.error("SHIP Status API HTTP %s (%s): %s", e.code, url, e.reason)
            return {"error": f"HTTP {e.code}: {e.reason}"}
        except (URLError, TimeoutError, json.JSONDecodeError) as e:
            logger.error("SHIP Status API request failed (%s): %s", url, e)
            return None


def _outage_is_active(raw: dict[str, Any]) -> bool:
    et = raw.get("end_time")
    return et is None or (isinstance(et, dict) and not et.get("Valid"))


def _truncate_json(obj: Any) -> Any:
    text = json.dumps(obj, default=str)
    if len(text) <= _MAX_RESPONSE_CHARS:
        return obj
    return {
        "error": f"Response truncated ({len(text)} chars exceeds {_MAX_RESPONSE_CHARS} limit).",
        "preview": text[:_MAX_RESPONSE_CHARS] + "...",
    }


class ShipStatusAPI:
    """Domain operations for MCP tools."""

    def __init__(self, client: DashboardClient | None = None) -> None:
        self.client = client or DashboardClient()

    def get_infrastructure_status(self) -> dict[str, Any]:
        data = self.client.public_get("/status")
        if data is None:
            return {"error": "Failed to retrieve status from SHIP Status Dashboard."}
        if not isinstance(data, list):
            return {"error": "Unexpected response shape from /api/status (expected a list)."}

        components = []
        all_healthy = True
        for entry in data:
            if not isinstance(entry, dict):
                continue
            status = entry.get("status", "Unknown")
            outages = entry.get("active_outages") or []
            if status != "Healthy":
                all_healthy = False
            components.append(
                {
                    "component_name": entry.get("component_name", "Unknown"),
                    "status": status,
                    "active_outage_count": len(outages),
                    "active_outages": [dict(o) for o in outages if isinstance(o, dict)],
                }
            )

        return _truncate_json(
            {
                "dashboard_url": self.client.dashboard_url,
                "overall_healthy": all_healthy,
                "components": components,
            }
        )

    def get_component_details(self, component_slug: str) -> dict[str, Any]:
        data = self.client.public_get(f"/components/{component_slug}")
        if data is None:
            return {
                "error": f"Failed to retrieve component '{component_slug}'. Check the slug is correct."
            }
        if isinstance(data, dict) and "error" in data:
            return data
        if not isinstance(data, dict):
            return {"error": "Unexpected response shape from component endpoint."}

        sub_components = []
        for sc in data.get("sub_components") or []:
            if isinstance(sc, dict):
                sub_components.append(
                    {
                        "name": sc.get("name"),
                        "slug": sc.get("slug"),
                        "description": sc.get("description", ""),
                    }
                )

        return _truncate_json(
            {
                "name": data.get("name"),
                "slug": data.get("slug"),
                "description": data.get("description", ""),
                "ship_team": data.get("ship_team", ""),
                "sub_components": sub_components,
            }
        )

    def get_outage(self, component_slug: str, sub_component_slug: str, outage_id: int) -> dict[str, Any]:
        path = f"/components/{component_slug}/{sub_component_slug}/outages/{outage_id}"
        data = self.client.public_get(path)
        if data is None:
            return {
                "error": (
                    f"Failed to retrieve outage {outage_id} for "
                    f"'{component_slug}/{sub_component_slug}'."
                )
            }
        if isinstance(data, dict) and "error" in data:
            return data
        if not isinstance(data, dict):
            return {"error": "Unexpected response shape from SHIP Status outage endpoint."}
        return _truncate_json(data)

    def _enrich_outages_slack_threads(
        self,
        component_slug: str,
        sub_component_slug: str,
        raw_list: list[dict[str, Any]],
    ) -> tuple[list[dict[str, Any]], str | None]:
        copies: list[dict[str, Any]] = [dict(x) for x in raw_list]
        if not copies:
            return [], None

        total = len(copies)
        note: str | None = None
        if total > _MAX_OUTAGES_SLACK_ENRICH:
            note = (
                f"The outages list omits slack_threads. Per-outage enrichment ran only for the first "
                f"{_MAX_OUTAGES_SLACK_ENRICH} of {total} outages (cap). Remaining rows were not merged. "
                "Use get_outage(component_slug, sub_component_slug, outage_id) for full records "
                "including slack_threads."
            )

        enrich_limit = min(total, _MAX_OUTAGES_SLACK_ENRICH)
        for i in range(enrich_limit):
            raw = copies[i]
            if raw.get("slack_threads"):
                continue
            oid = raw.get("ID", raw.get("id"))
            sub = (sub_component_slug or raw.get("sub_component_name") or "").strip()
            if oid is None or not sub:
                continue
            detail = self.client.public_get(f"/components/{component_slug}/{sub}/outages/{oid}")
            if isinstance(detail, dict) and not detail.get("error") and detail.get("slack_threads"):
                merged = dict(raw)
                merged["slack_threads"] = detail["slack_threads"]
                copies[i] = merged
        return copies, note

    def get_component_outages(
        self, component_slug: str, sub_component_slug: str = ""
    ) -> dict[str, Any]:
        if sub_component_slug:
            path = f"/components/{component_slug}/{sub_component_slug}/outages"
        else:
            path = f"/components/{component_slug}/outages"

        data = self.client.public_get(path)
        if data is None:
            return {
                "error": f"Failed to retrieve outages for '{component_slug}'. Check the slug is correct."
            }
        if isinstance(data, dict) and "error" in data:
            return data
        if not isinstance(data, list):
            sub_disp = sub_component_slug or "(all)"
            preview = repr(data)
            if len(preview) > 400:
                preview = preview[:400] + "..."
            return {
                "error": (
                    f"SHIP Status outages list for component={component_slug!r} "
                    f"sub_component={sub_disp!r} expected a JSON array, "
                    f"got {type(data).__name__}: {preview}"
                ),
            }

        raw_list = [x for x in data if isinstance(x, dict)]
        outages, enrich_note = self._enrich_outages_slack_threads(
            component_slug, sub_component_slug, raw_list
        )

        active = [o for o in outages if _outage_is_active(o)]
        resolved = [o for o in outages if not _outage_is_active(o)]

        out: dict[str, Any] = {
            "component": component_slug,
            "sub_component": sub_component_slug or "(all)",
            "total_outages": len(outages),
            "active_count": len(active),
            "resolved_count": len(resolved),
            "outages": outages,
        }
        if enrich_note:
            out["enrichment_note"] = enrich_note
        return _truncate_json(out)

    def get_outages_during(
        self,
        start: str = "",
        end: str = "",
        component_name: str = "",
        sub_component_name: str = "",
        tag: str = "",
        team: str = "",
    ) -> dict[str, Any]:
        start_s = (start or "").strip()
        end_s = (end or "").strip()
        if not start_s and not end_s:
            return {
                "error": (
                    "Provide at least one of start or end (RFC3339 or RFC3339Nano UTC), "
                    "e.g. 2026-05-13T14:22:06Z"
                )
            }

        comp = (component_name or "").strip()
        sub = (sub_component_name or "").strip()
        if sub and not comp:
            return {
                "error": "sub_component_name requires component_name (API rejects subComponentName alone)."
            }

        params: list[tuple[str, str]] = []
        if start_s:
            params.append(("start", start_s))
        if end_s:
            params.append(("end", end_s))
        if comp:
            params.append(("componentName", comp))
        if sub:
            params.append(("subComponentName", sub))
        tag_s = (tag or "").strip()
        if tag_s:
            params.append(("tag", tag_s))
        team_s = (team or "").strip()
        if team_s:
            params.append(("team", team_s))

        path = f"/outages/during?{urlencode(params)}"
        data = self.client.public_get(path)
        if data is None:
            return {
                "error": (
                    "Failed to retrieve outages from SHIP Status /api/outages/during "
                    "(network or server error)."
                )
            }
        if isinstance(data, dict) and "error" in data:
            return data
        if isinstance(data, list):
            return _truncate_json({"outages": data, "count": len(data)})
        return {"error": "Unexpected JSON shape from /api/outages/during (expected a list)."}

    def list_components(self) -> dict[str, Any]:
        data = self.client.public_get("/components")
        if data is None:
            return {"error": "Failed to retrieve components list."}
        if isinstance(data, dict) and "error" in data:
            return data
        return _truncate_json({"components": data})

    def list_tags(self) -> dict[str, Any]:
        data = self.client.public_get("/tags")
        if data is None:
            return {"error": "Failed to retrieve tags list."}
        if isinstance(data, dict) and "error" in data:
            return data
        return _truncate_json({"tags": data})

    def list_sub_components(self, status: str = "") -> dict[str, Any]:
        path = "/sub-components"
        status_s = (status or "").strip()
        if status_s:
            statuses = [s.strip() for s in status_s.split(",") if s.strip()]
            if not statuses:
                return {"error": "status filter must include at least one status"}
            path = f"/sub-components?{urlencode([('status', s) for s in statuses])}"
        data = self.client.public_get(path)
        if data is None:
            return {"error": "Failed to retrieve sub-components list."}
        if isinstance(data, dict) and "error" in data:
            return data
        return _truncate_json({"sub_components": data})

    def get_team_slo(self, team: str) -> dict[str, Any]:
        data = self.client.public_get(f"/teams/{quote(team, safe='')}/slo")
        if data is None:
            return {"error": f"Failed to retrieve SLO for team {team}."}
        if isinstance(data, dict) and "error" in data:
            return data
        if isinstance(data, dict):
            return data
        return {"error": "Unexpected JSON shape from team SLO (expected an object)."}

    def get_team_slo_summary(self) -> dict[str, Any]:
        data = self.client.public_get("/teams/slo-summary")
        if data is None:
            return {"error": "Failed to retrieve SLO summary."}
        if isinstance(data, dict) and "error" in data:
            return data
        if isinstance(data, dict):
            return data
        return {"error": "Unexpected JSON shape from SLO summary (expected an object)."}

    # Write operations (protected API)

    def _protected_error(self, data: dict | list | None, fallback: str) -> dict[str, Any] | None:
        """Return an error dict if the response indicates failure, or None on success."""
        if data is None:
            return {"error": fallback}
        if isinstance(data, dict) and "error" in data:
            return data
        return None

    def _dict_request(
        self, method: str, path: str, body: dict[str, Any] | None, fallback_msg: str,
        truncate: bool = False, acting_for: str = "",
    ) -> dict[str, Any]:
        """Issue a protected request expecting a dict response. Returns error dict on failure."""
        data = self.client.protected_request(method, path, body=body, acting_for=acting_for)
        if err := self._protected_error(data, fallback_msg):
            return err
        if not isinstance(data, dict):
            return {"error": f"Unexpected response shape from {method} {path}."}
        return _truncate_json(data) if truncate else data

    def _delete_request(self, path: str, success_msg: str, acting_for: str = "") -> dict[str, Any]:
        """Issue a protected DELETE request. None or non-error response means success."""
        data = self.client.protected_request("DELETE", path, acting_for=acting_for)
        if isinstance(data, dict) and "error" in data:
            return data
        return {"success": True, "message": success_msg}

    def _find_active_outage(self, component_slug: str, sub_component_slug: str) -> dict[str, Any] | None:
        """Return the first active outage for a sub-component, or None if there are no active outages."""
        path = f"/components/{component_slug}/{sub_component_slug}/outages"
        data = self.client.public_get(path)
        if not isinstance(data, list):
            return None
        for entry in data:
            if isinstance(entry, dict) and _outage_is_active(entry):
                return entry
        return None

    def create_outage(
        self,
        component_slug: str,
        sub_component_slug: str,
        severity: str,
        description: str,
        start_time: str = "",
        initial_triage_note: str = "",
        bot_initiated: bool = False,
        acting_for: str = "",
    ) -> dict[str, Any]:
        existing = self._find_active_outage(component_slug, sub_component_slug)
        if existing:
            if initial_triage_note and existing.get("ID"):
                note_result = self.add_triage_note(
                    component_slug, sub_component_slug, existing["ID"], initial_triage_note,
                    acting_for=acting_for,
                )
                if "error" in note_result:
                    return note_result
            return _truncate_json({
                "existing_outage": True,
                "message": "Active outage already exists for this sub-component.",
                "outage": existing,
            })

        warning = ""
        if bot_initiated:
            if severity != "Degraded":
                warning = f"Severity overridden from {severity!r} to 'Degraded' for bot-initiated outage."
            severity = "Degraded"
            confirmed = False
        else:
            confirmed = True

        body: dict[str, Any] = {
            "severity": severity,
            "description": description,
            "discovered_from": "mcp",
            "confirmed": confirmed,
            "start_time": start_time or datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        }
        if initial_triage_note:
            body["initial_triage_note"] = initial_triage_note

        path = f"/components/{component_slug}/{sub_component_slug}/outages"
        result = self._dict_request("POST", path, body, "Failed to create outage.", truncate=True, acting_for=acting_for)
        if warning and "error" not in result:
            result["warning"] = warning
        return result

    def update_outage(
        self,
        component_slug: str,
        sub_component_slug: str,
        outage_id: int,
        severity: str = "",
        description: str = "",
        start_time: str = "",
        end_time: str = "",
        confirmed: bool | None = None,
        acting_for: str = "",
    ) -> dict[str, Any]:
        body: dict[str, Any] = {}
        if severity:
            body["severity"] = severity
        if description:
            body["description"] = description
        if start_time:
            body["start_time"] = start_time
        if end_time:
            body["end_time"] = {"Time": end_time, "Valid": True}
        if confirmed is not None:
            body["confirmed"] = confirmed

        if not body:
            return {"error": "No fields to update. Provide at least one of: severity, description, start_time, end_time, confirmed."}

        path = f"/components/{component_slug}/{sub_component_slug}/outages/{outage_id}"
        return self._dict_request("PATCH", path, body, f"Failed to update outage {outage_id}.", truncate=True, acting_for=acting_for)

    def delete_outage(
        self,
        component_slug: str,
        sub_component_slug: str,
        outage_id: int,
        acting_for: str = "",
    ) -> dict[str, Any]:
        path = f"/components/{component_slug}/{sub_component_slug}/outages/{outage_id}"
        return self._delete_request(path, f"Outage {outage_id} deleted.", acting_for=acting_for)

    def add_triage_note(
        self,
        component_slug: str,
        sub_component_slug: str,
        outage_id: int,
        body: str,
        acting_for: str = "",
    ) -> dict[str, Any]:
        req_body: dict[str, Any] = {"body": body}
        path = f"/components/{component_slug}/{sub_component_slug}/outages/{outage_id}/triage-notes"
        return self._dict_request("POST", path, req_body, f"Failed to add triage note to outage {outage_id}.", acting_for=acting_for)

    def update_triage_note(
        self,
        component_slug: str,
        sub_component_slug: str,
        outage_id: int,
        note_id: int,
        body: str,
        acting_for: str = "",
    ) -> dict[str, Any]:
        req_body: dict[str, Any] = {"body": body}
        path = f"/components/{component_slug}/{sub_component_slug}/outages/{outage_id}/triage-notes/{note_id}"
        return self._dict_request("PATCH", path, req_body, f"Failed to update triage note {note_id}.", acting_for=acting_for)

    def delete_triage_note(
        self,
        component_slug: str,
        sub_component_slug: str,
        outage_id: int,
        note_id: int,
        acting_for: str = "",
    ) -> dict[str, Any]:
        path = f"/components/{component_slug}/{sub_component_slug}/outages/{outage_id}/triage-notes/{note_id}"
        return self._delete_request(path, f"Triage note {note_id} deleted.", acting_for=acting_for)

    def add_outage_link(
        self,
        component_slug: str,
        sub_component_slug: str,
        outage_id: int,
        url: str,
        link_type: str = "other",
        description: str = "",
        acting_for: str = "",
    ) -> dict[str, Any]:
        link_body: dict[str, Any] = {"url": url, "link_type": link_type}
        if description:
            link_body["description"] = description
        path = f"/components/{component_slug}/{sub_component_slug}/outages/{outage_id}/links"
        return self._dict_request("POST", path, link_body, f"Failed to add link to outage {outage_id}.", acting_for=acting_for)

    def update_outage_link(
        self,
        component_slug: str,
        sub_component_slug: str,
        outage_id: int,
        link_id: int,
        url: str,
        link_type: str = "other",
        description: str = "",
        acting_for: str = "",
    ) -> dict[str, Any]:
        link_body: dict[str, Any] = {"url": url, "link_type": link_type}
        if description:
            link_body["description"] = description
        path = f"/components/{component_slug}/{sub_component_slug}/outages/{outage_id}/links/{link_id}"
        return self._dict_request("PATCH", path, link_body, f"Failed to update link {link_id}.", acting_for=acting_for)

    def delete_outage_link(
        self,
        component_slug: str,
        sub_component_slug: str,
        outage_id: int,
        link_id: int,
        acting_for: str = "",
    ) -> dict[str, Any]:
        path = f"/components/{component_slug}/{sub_component_slug}/outages/{outage_id}/links/{link_id}"
        return self._delete_request(path, f"Link {link_id} deleted.", acting_for=acting_for)

    def add_outage_relationship(
        self,
        component_slug: str,
        sub_component_slug: str,
        outage_id: int,
        related_outage_id: int,
        relationship_type: str = "related_to",
        acting_for: str = "",
    ) -> dict[str, Any]:
        body: dict[str, Any] = {"related_outage_id": related_outage_id, "relationship_type": relationship_type}
        path = f"/components/{component_slug}/{sub_component_slug}/outages/{outage_id}/relationships"
        return self._dict_request("POST", path, body, f"Failed to add relationship to outage {outage_id}.", acting_for=acting_for)

    def delete_outage_relationship(
        self,
        component_slug: str,
        sub_component_slug: str,
        outage_id: int,
        relationship_id: int,
        acting_for: str = "",
    ) -> dict[str, Any]:
        path = f"/components/{component_slug}/{sub_component_slug}/outages/{outage_id}/relationships/{relationship_id}"
        return self._delete_request(path, f"Relationship {relationship_id} deleted.", acting_for=acting_for)

    def upsert_slo_item(
        self,
        team: str,
        kind: str,
        schema_version: int,
        item_key: str,
        group_key: str,
        occurred_at: str,
        outcome: str,
        details: dict,
        notes: str = "",
        acting_for: str = "",
    ) -> dict[str, Any]:
        body = {
            "kind": kind,
            "schema_version": schema_version,
            "item_key": item_key,
            "group_key": group_key,
            "occurred_at": occurred_at,
            "outcome": outcome,
            "details": details,
            "notes": notes,
        }
        path = f"/teams/{quote(team, safe='')}/slo/items"
        return self._dict_request("PUT", path, body, "Failed to upsert SLO item.", acting_for=acting_for)

    def add_slo_item_link(
        self,
        team: str,
        kind: str,
        item_key: str,
        url: str,
        link_type: str = "other",
        outage_id: int | None = None,
        acting_for: str = "",
    ) -> dict[str, Any]:
        body: dict[str, Any] = {"url": url, "link_type": link_type}
        if outage_id is not None:
            body["outage_id"] = outage_id
        path = (
            f"/teams/{quote(team, safe='')}/slo/items/"
            f"{quote(kind, safe='')}/{quote(item_key, safe='')}/links"
        )
        return self._dict_request("PUT", path, body, "Failed to add SLO item link.", acting_for=acting_for)
