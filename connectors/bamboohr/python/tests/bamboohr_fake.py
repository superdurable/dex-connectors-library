# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""A credential-checking BambooHR fake, ported from the Go connector's internal/bamboohrfake.

It serves Get Changed Employee IDs and Get Employee, keeps one change per employee as BambooHR does, and
stamps changes with its own clock, which it also sends in the Date header. The clock advances one second
per change and by `advance`, as BambooHR's does between polls.
"""

import base64
import json
import threading
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from email.utils import format_datetime
from typing import Any

import httpx

# Split so secret scanners do not mistake the 40-hex fixture for a real key.
API_KEY = "0123456789abcdef" + "0123456789abcdef01234567"
START = datetime(2026, 10, 13, 9, 0, 0, tzinfo=UTC)


@dataclass
class _Employee:
    fields: dict[str, str] = field(default_factory=dict)
    action: str = ""
    last_changed: datetime = START
    is_deleted: bool = False


class BambooHRFake:
    """An httpx.MockTransport handler over the BambooHR API v1 endpoints the Trigger and example use."""

    def __init__(self, start: datetime = START) -> None:
        self._lock = threading.Lock()
        self._clock = start.astimezone(UTC).replace(microsecond=0)
        self._employees: dict[str, _Employee] = {}
        self.change_list_queries: list[dict[str, str]] = []
        self._rate_limited_change_lists = 0
        self._rejected_change_lists = 0

    def insert(self, employee_id: str, fields: dict[str, str] | None = None) -> None:
        """Add an employee one second after the previous change, as an Inserted change."""
        self._change(employee_id, "Inserted", fields)

    def update(self, employee_id: str, fields: dict[str, str] | None = None) -> None:
        """Merge fields into an employee one second after the previous change, as an Updated change."""
        self._change(employee_id, "Updated", fields)

    def delete(self, employee_id: str) -> None:
        """Delete an employee one second after the previous change, as a Deleted change."""
        self._change(employee_id, "Deleted", None)

    def advance(self, duration: timedelta) -> None:
        """Move the clock, and so the Date header, forward without changing any employee."""
        with self._lock:
            self._clock = (self._clock + duration).replace(microsecond=0)

    def insert_late(self, employee_id: str, last_changed: datetime) -> None:
        """Add an employee stamped last_changed without moving the clock, as a late-visible change."""
        with self._lock:
            stamp = last_changed.astimezone(UTC).replace(microsecond=0)
            self._employees[employee_id] = _Employee(action="Inserted", last_changed=stamp)

    def rate_limit_change_lists(self, count: int) -> None:
        """Answer the next count change-list requests with 429 and Retry-After: 1."""
        with self._lock:
            self._rate_limited_change_lists = count

    def reject_change_lists(self, count: int) -> None:
        """Answer the next count change-list requests with 400."""
        with self._lock:
            self._rejected_change_lists = count

    def clock(self) -> datetime:
        """Return the fake's current time, which the Date header also reports."""
        with self._lock:
            return self._clock

    def _change(self, employee_id: str, action: str, fields: dict[str, str] | None) -> None:
        with self._lock:
            self._clock += timedelta(seconds=1)
            record = self._employees.setdefault(employee_id, _Employee())
            record.fields.update(fields or {})
            record.action, record.last_changed = action, self._clock
            record.is_deleted = action == "Deleted"

    def handle(self, request: httpx.Request) -> httpx.Response:
        """Answer one request."""
        with self._lock:
            headers = {"Date": format_datetime(self._clock, usegmt=True)}
            expected = "Basic " + base64.b64encode(f"{API_KEY}:x".encode()).decode()
            path = request.url.path
            if request.headers.get("Authorization") != expected:
                return httpx.Response(401, headers=headers)
            if request.method == "GET" and path == "/api/v1/employees/changed":
                return self._serve_change_list(headers, dict(request.url.params))
            if request.method == "GET" and path.startswith("/api/v1/employees/"):
                fields = request.url.params.get("fields", "")
                return self._serve_employee(headers, path.removeprefix("/api/v1/employees/"), fields)
            return httpx.Response(404, headers=headers)

    def _serve_change_list(self, headers: dict[str, str], query: dict[str, str]) -> httpx.Response:
        self.change_list_queries.append(query)
        if self._rate_limited_change_lists > 0:
            self._rate_limited_change_lists -= 1
            return httpx.Response(429, headers={**headers, "Retry-After": "1"})
        if self._rejected_change_lists > 0:
            self._rejected_change_lists -= 1
            return _json(400, headers, {"error": {"code": "BadRequest"}})
        try:
            since = datetime.fromisoformat(query.get("since", ""))
        except ValueError:
            return httpx.Response(400, headers=headers)
        change_type = query.get("type", "")
        if change_type not in ("", "inserted", "updated", "deleted", "all"):
            return httpx.Response(400, headers=headers)
        employees: dict[str, Any] = {}
        latest = ""
        for employee_id, record in self._employees.items():
            if record.last_changed <= since:
                continue
            if change_type not in ("", "all") and record.action.lower() != change_type:
                continue
            stamp = record.last_changed.strftime("%Y-%m-%dT%H:%M:%S+00:00")
            employees[employee_id] = {"id": employee_id, "action": record.action, "lastChanged": stamp}
            latest = max(latest, stamp)
        return _json(200, headers, {"latest": latest, "employees": employees or []})

    def _serve_employee(self, headers: dict[str, str], employee_id: str, fields: str) -> httpx.Response:
        record = self._employees.get(employee_id)
        if not employee_id.isdigit() or record is None or record.is_deleted:
            return httpx.Response(404, headers=headers)
        body = {"id": employee_id}
        for name in fields.split(","):
            if name in record.fields:
                body[name] = record.fields[name]
        return _json(200, headers, body)


def _json(status: int, headers: dict[str, str], body: Any) -> httpx.Response:
    return httpx.Response(
        status, headers={**headers, "Content-Type": "application/json"}, content=json.dumps(body).encode()
    )
