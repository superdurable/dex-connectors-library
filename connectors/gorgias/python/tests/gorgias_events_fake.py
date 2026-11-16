# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""A stateful fake of Gorgias's GET /api/events and GET /api/tickets/{id}, ported from the Go connector's
gorgiaseventfake. It stamps each event's created_datetime from the real clock at microsecond precision
without an offset, as Gorgias documents, unless a test passes an explicit time."""

import base64
import json
import threading
from dataclasses import dataclass, field
from datetime import UTC, datetime
from email.utils import format_datetime
from typing import Any

import httpx

EMAIL = "agent@example.com"
# Split so secret scanners do not mistake the fake's key for a real credential.
API_KEY = "gorgiasFake" + "Key0123456789"
_TIME_LAYOUT = "%Y-%m-%dT%H:%M:%S.%f"
_CREATED = "2026-10-20T09:00:00.000000"


@dataclass
class _StoredEvent:
    id: int
    event_type: str
    object_type: str
    object_id: int
    user_id: int
    created_at: datetime


@dataclass
class _StoredTicket:
    id: int
    subject: str
    status: str
    tags: tuple[str, ...] = ()


@dataclass
class _InjectedFailure:
    status: int
    retry_after: str
    body: str


@dataclass
class GorgiasEventsFake:
    """A Gorgias event log and ticket store, safe for use from the Worker's threads."""

    email: str = EMAIL
    api_key: str = API_KEY
    next_event_id: int = 9000
    api_call_limit: str = "1/40"
    event_request_count: int = 0
    _events: list[_StoredEvent] = field(default_factory=list)
    _tickets: dict[int, _StoredTicket] = field(default_factory=dict)
    _failures: list[_InjectedFailure] = field(default_factory=list)
    _lock: threading.Lock = field(default_factory=threading.Lock)

    def set_ticket(self, ticket_id: int, subject: str, status: str, *tags: str) -> None:
        """Store the ticket GET /api/tickets/{id} returns, without recording an event."""
        with self._lock:
            self._tickets[ticket_id] = _StoredTicket(ticket_id, subject, status, tags)

    def record_ticket_event(self, event_type: str, ticket_id: int, user_id: int = 0) -> int:
        """Append an event about ticket_id created now by user_id (0 for a rule) and return its ID."""
        return self.record_event_at(event_type, "Ticket", ticket_id, user_id, datetime.now(UTC))

    def record_event_at(
        self, event_type: str, object_type: str, object_id: int, user_id: int, created_at: datetime
    ) -> int:
        """Append an event with an explicit object and created_datetime and return its ID."""
        with self._lock:
            self.next_event_id += 1
            self._events.append(
                _StoredEvent(self.next_event_id, event_type, object_type, object_id, user_id, created_at)
            )
            return self.next_event_id

    def fail_next_events(self, status: int, retry_after: str, body: str) -> None:
        """Answer the next GET /api/events with status, an optional Retry-After value, and body."""
        with self._lock:
            self._failures.append(_InjectedFailure(status, retry_after, body))

    def handle(self, request: httpx.Request) -> httpx.Response:
        """Serve one request as Gorgias would."""
        with self._lock:
            headers = {
                "Date": format_datetime(datetime.now(UTC), usegmt=True),
                "X-Gorgias-Account-Api-Call-Limit": self.api_call_limit,
            }
            if not self._is_authorized(request):
                return _json(401, {"error": {"msg": "invalid_credentials"}}, headers)
            if request.method != "GET":
                return _json(405, {"error": {"msg": "method_not_allowed"}}, headers)
            path = request.url.path.removeprefix("/api")
            if path == "/events":
                self.event_request_count += 1
                return self._list_events(request.url.params, headers)
            ticket_text = path.removeprefix("/tickets/")
            ticket = self._tickets.get(int(ticket_text)) if ticket_text.isdecimal() else None
            if ticket is None:
                return _json(404, {"error": {"msg": "not_found"}}, headers)
            return _json(200, _ticket_wire(ticket), headers)

    def _is_authorized(self, request: httpx.Request) -> bool:
        expected = base64.b64encode(f"{self.email}:{self.api_key}".encode()).decode()
        return bool(request.headers.get("Authorization", "") == "Basic " + expected)

    def _list_events(self, query: httpx.QueryParams, headers: dict[str, str]) -> httpx.Response:
        if self._failures:
            failure = self._failures.pop(0)
            if failure.retry_after:
                headers["Retry-After"] = failure.retry_after
            headers["Content-Type"] = "application/json"
            return httpx.Response(failure.status, headers=headers, content=failure.body.encode())
        types = query.get_list("types")
        try:
            since = datetime.fromisoformat(query.get("created_datetime[gte]", ""))
            limit = int(query.get("limit", ""))
        except ValueError:
            since, limit = None, 0
        if (
            since is None
            or not 1 <= limit <= 100
            or query.get("order_by") != "created_datetime:asc"
            or not types
        ):
            body = {"error": {"msg": "invalid_query", "data": {"created_datetime": ["bad"]}}}
            return _json(400, body, headers)
        matched = sorted(
            (event for event in self._events if event.created_at >= since and event.event_type in types),
            key=lambda event: event.created_at,
        )
        return _json(200, [_event_wire(event) for event in matched[:limit]], headers)


def _event_wire(event: _StoredEvent) -> dict[str, Any]:
    return {
        "id": event.id,
        "type": event.event_type,
        "object_type": event.object_type,
        "object_id": event.object_id,
        "user_id": event.user_id if event.user_id > 0 else None,
        "created_datetime": event.created_at.astimezone(UTC).strftime(_TIME_LAYOUT),
        "data": None,
        "context": "dd4ff312-69df-494a-be96-1a58b3d8b8e0",
        "uri": f"/api/events/{event.id}/",
    }


def _ticket_wire(ticket: _StoredTicket) -> dict[str, Any]:
    return {
        "id": ticket.id,
        "subject": ticket.subject,
        "status": ticket.status,
        "priority": "normal",
        "channel": "email",
        "customer": {"id": 501, "email": "ada@example.com", "name": "Ada Lovelace"},
        "tags": [{"name": tag} for tag in ticket.tags],
        "created_datetime": _CREATED,
        "updated_datetime": _CREATED,
        "messages": [
            {
                "id": ticket.id * 10 + 1,
                "ticket_id": ticket.id,
                "channel": "email",
                "via": "email",
                "public": True,
                "from_agent": False,
                "sender": {"id": 501},
                "subject": ticket.subject,
                "body_text": "Customer message for " + ticket.subject,
                "created_datetime": _CREATED,
            }
        ],
    }


def _json(status: int, value: Any, headers: dict[str, str]) -> httpx.Response:
    return httpx.Response(
        status, headers={**headers, "Content-Type": "application/json"}, content=json.dumps(value).encode()
    )
