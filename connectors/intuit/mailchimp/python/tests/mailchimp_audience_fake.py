# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""A fake of one Mailchimp audience's contact list and contact reads, as an httpx transport handler.

It models the documented list semantics the memberChanged Trigger relies on, as the Go connector's
mailchimpaudiencefake does: since_last_changed keeps contacts changed strictly after the bound,
sort_field=last_changed orders by change second, count is at most 1000, offset skips matching contacts,
and fields keeps only the listed members.* fields.
"""

import hashlib
import json
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any

import httpx

# Split so secret scanners do not read the fixture as a credential; the -us6 suffix selects a data center.
API_KEY = "0123456789abcdef" + "0123456789abcdef" + "-us6"
API_BASE_URL = "https://mailchimp.example/3.0"


@dataclass(frozen=True, slots=True)
class Failure:
    """One injected response for the next list request."""

    status: int
    retry_after: str = ""
    without_document: bool = False
    """Send an empty body, as Mailchimp's throttling 403 does."""


def subscriber_hash(email_address: str) -> str:
    """Return Mailchimp's member ID for email_address."""
    return hashlib.md5(email_address.lower().encode()).hexdigest()  # noqa: S324 -- Mailchimp's address key.


class MailchimpAudienceFake:
    """One fake audience whose clock starts at 2026-11-01T09:00:00Z; handle is the transport handler."""

    def __init__(self, audience_id: str) -> None:
        self.audience_id = audience_id
        self.clock = datetime(2026, 11, 1, 9, 0, 0, tzinfo=UTC)
        self._members: dict[str, tuple[dict[str, Any], datetime]] = {}
        self._failures: list[Failure] = []
        self.requests: list[str] = []

    def upsert(self, email_address: str, status: str, first_name: str) -> str:
        """Add or change the contact one second after the previous change and return its hash."""
        self.clock += timedelta(seconds=1)
        return self.upsert_at(email_address, status, first_name, self.clock)

    def upsert_at(self, email_address: str, status: str, first_name: str, changed_at: datetime) -> str:
        """Add or change the contact with an explicit change time, which may lie before earlier changes."""
        changed_at = changed_at.astimezone(UTC).replace(microsecond=0)
        member_id = subscriber_hash(email_address)
        wire = {
            "id": member_id,
            "email_address": email_address,
            "status": status,
            "merge_fields": {"FNAME": first_name},
            "last_changed": changed_at.strftime("%Y-%m-%dT%H:%M:%S+00:00"),
            "list_id": self.audience_id,
        }
        self._members[member_id] = (wire, changed_at)
        return member_id

    def fail_next_list(self, *failures: Failure) -> None:
        """Answer the next list requests with failures, in order."""
        self._failures.extend(failures)

    def handle(self, request: httpx.Request) -> httpx.Response:
        """Serve one authenticated request."""
        if request.headers.get("Authorization") != "Bearer " + API_KEY:
            return _problem(401, "API Key Invalid")
        self.requests.append(f"{request.method} {request.url.path}?{request.url.query.decode()}")
        prefix = f"/3.0/lists/{self.audience_id}/members"
        if request.method != "GET":
            return _problem(405, "Method Not Allowed")
        if request.url.path == prefix:
            return self._serve_list(request)
        if request.url.path.startswith(prefix + "/"):
            found = self._members.get(request.url.path.removeprefix(prefix + "/"))
            return httpx.Response(200, json=found[0]) if found else _problem(404, "Resource Not Found")
        return _problem(404, "Resource Not Found")

    def _serve_list(self, request: httpx.Request) -> httpx.Response:
        if self._failures:
            failure = self._failures.pop(0)
            headers = {"Retry-After": failure.retry_after} if failure.retry_after else {}
            if failure.without_document:
                return httpx.Response(failure.status, headers=headers)
            return _problem(failure.status, "Injected failure", headers)
        params = request.url.params
        try:
            count, offset = int(params.get("count", "")), int(params.get("offset", ""))
        except ValueError:
            return _problem(400, "Invalid Resource")
        if not 1 <= count <= 1000 or offset < 0 or params.get("sort_field") != "last_changed":
            return _problem(400, "Invalid Resource")
        since = None
        if params.get("since_last_changed"):
            since = datetime.fromisoformat(params["since_last_changed"])
        matching = [item for item in self._members.values() if since is None or item[1] > since]
        descending = params.get("sort_dir") == "DESC"
        matching.sort(key=lambda item: item[0]["id"])
        matching.sort(key=lambda item: item[1], reverse=descending)
        page = matching[offset : offset + count]
        fields = [field.removeprefix("members.") for field in params.get("fields", "").split(",") if field]
        members = [
            {name: value for name, value in wire.items() if not fields or name in fields} for wire, _ in page
        ]
        body = {"members": members, "list_id": self.audience_id, "total_items": len(matching)}
        return httpx.Response(200, json=body)


def _problem(status: int, title: str, headers: dict[str, str] | None = None) -> httpx.Response:
    body = json.dumps({"type": "https://mailchimp.com/developer/marketing/docs/errors/", "title": title})
    return httpx.Response(status, headers=headers, content=body.encode())
