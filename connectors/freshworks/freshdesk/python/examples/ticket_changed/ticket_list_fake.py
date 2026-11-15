# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""A stateful Freshdesk ticket store served through httpx for ticketChanged tests.

It serves List All Tickets, View a Ticket, and List All Conversations as the Go connector's
freshdeskticketfake does, and stamps created_at and updated_at from the real clock at one-second
precision, as Freshdesk does, unless a test passes an explicit time.
"""

import base64
import json
import threading
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from email.utils import format_datetime
from typing import Any
from urllib.parse import urlencode

import httpx

_TIME_LAYOUT = "%Y-%m-%dT%H:%M:%SZ"
_MAXIMUM_LIST_PAGE = 300


@dataclass(slots=True)
class _StoredTicket:
    ticket_id: int
    subject: str
    status: int
    created_at: datetime
    updated_at: datetime


@dataclass(frozen=True, slots=True)
class _InjectedFailure:
    status: int
    retry_after: str
    body: str


def _now_second() -> datetime:
    return datetime.now(UTC).replace(microsecond=0)


class FreshdeskTicketFake:
    """A Freshdesk ticket store that accepts one API key over HTTP Basic; safe across threads."""

    base_url = "https://acme.freshdesk.com/api/v2"
    """The base URL to pass as the client's `api_base_url`."""

    def __init__(self, api_key: str) -> None:
        """Start an empty store for api_key."""
        self._authorization = "Basic " + base64.b64encode(f"{api_key}:X".encode()).decode()
        self._lock = threading.Lock()
        self._tickets: dict[int, _StoredTicket] = {}
        self._failures: list[_InjectedFailure] = []
        self._list_queries: list[dict[str, str]] = []
        self._clock_skew = timedelta(0)

    def transport(self) -> httpx.MockTransport:
        """Return an httpx transport that serves this store."""
        return httpx.MockTransport(self._serve)

    def create(self, ticket_id: int, subject: str) -> None:
        """Add an Open ticket created and updated now."""
        now = _now_second()
        self.create_at(ticket_id, subject, now, now)

    def create_at(self, ticket_id: int, subject: str, created_at: datetime, updated_at: datetime) -> None:
        """Add an Open ticket with explicit created_at and updated_at times."""
        with self._lock:
            self._tickets[ticket_id] = _StoredTicket(
                ticket_id, subject, 2, created_at.astimezone(UTC), updated_at.astimezone(UTC)
            )

    def update(self, ticket_id: int, subject: str, status: int) -> None:
        """Change a ticket's subject and status and stamp updated_at at least one second later."""
        with self._lock:
            ticket = self._tickets[ticket_id]
            ticket.subject, ticket.status = subject, status
            ticket.updated_at = max(_now_second(), ticket.updated_at + timedelta(seconds=1))

    def advance_clock(self, duration: timedelta) -> None:
        """Move the time the Date header reports, as minutes passing on Freshdesk's clock."""
        with self._lock:
            self._clock_skew += duration

    def fail_next_list(self, status: int, retry_after: str, body: str) -> None:
        """Answer the next List All Tickets request with status, Retry-After, and an error body."""
        with self._lock:
            self._failures.append(_InjectedFailure(status, retry_after, body))

    def list_request_count(self) -> int:
        """Return how many List All Tickets requests arrived."""
        with self._lock:
            return len(self._list_queries)

    def list_queries(self) -> list[dict[str, str]]:
        """Return the List All Tickets query parameters in arrival order."""
        with self._lock:
            return list(self._list_queries)

    def _serve(self, request: httpx.Request) -> httpx.Response:
        with self._lock:
            date = format_datetime(datetime.now(UTC) + self._clock_skew, usegmt=True)
            headers = {"Date": date, "Content-Type": "application/json"}
            if request.headers.get("Authorization") != self._authorization:
                return _json(401, {"code": "invalid_credentials"}, headers)
            if request.method != "GET":
                return _json(405, {"code": "method_not_allowed"}, headers)
            path = request.url.path.removeprefix("/api/v2")
            if path == "/tickets":
                query = dict(request.url.params)
                self._list_queries.append(query)
                return self._list_tickets(query, headers)
            return self._view_ticket(path, headers)

    def _view_ticket(self, path: str, headers: dict[str, str]) -> httpx.Response:
        parts = path.removeprefix("/tickets/").split("/")
        ticket = self._tickets.get(int(parts[0])) if parts[0].isdigit() else None
        if ticket is None or len(parts) > 2 or (len(parts) == 2 and parts[1] != "conversations"):
            return _json(404, {"code": "access_denied"}, headers)
        if len(parts) == 2:
            return _json(200, [], headers)
        wire = _ticket_wire(ticket)
        wire["description_text"] = "Customer message for " + ticket.subject
        wire["requester"] = {"id": 6007738334, "name": "Jane Smith", "email": "jane@acme.example.com"}
        return _json(200, wire, headers)

    def _list_tickets(self, query: dict[str, str], headers: dict[str, str]) -> httpx.Response:
        if self._failures:
            failure = self._failures.pop(0)
            if failure.retry_after:
                headers["Retry-After"] = failure.retry_after
            return httpx.Response(failure.status, headers=headers, content=failure.body.encode())
        try:
            since = datetime.strptime(query.get("updated_since", ""), _TIME_LAYOUT).replace(tzinfo=UTC)
            per_page, page = int(query.get("per_page", "")), int(query.get("page", ""))
        except ValueError:
            since, per_page, page = None, 0, 0
        if (
            since is None
            or not 1 <= per_page <= 100
            or page < 1
            or query.get("order_by") != "updated_at"
            or query.get("order_type") != "asc"
        ):
            invalid = {"field": "updated_since", "code": "invalid_value", "message": "bad"}
            return _json(400, {"description": "Validation failed", "errors": [invalid]}, headers)
        matched = sorted(
            (ticket for ticket in self._tickets.values() if ticket.updated_at >= since),
            key=lambda ticket: (ticket.updated_at, ticket.ticket_id),
        )
        start = min((page - 1) * per_page, len(matched))
        end = min(start + per_page, len(matched))
        # Freshdesk returns at most 300 pages (30,000 tickets) for one list query.
        if end < len(matched) and page < _MAXIMUM_LIST_PAGE:
            next_query = urlencode({**query, "page": page + 1})
            headers["Link"] = f'<{self.base_url}/tickets?{next_query}>; rel="next"'
        return _json(200, [_ticket_wire(ticket) for ticket in matched[start:end]], headers)


def _ticket_wire(ticket: _StoredTicket) -> dict[str, Any]:
    return {
        "id": ticket.ticket_id,
        "subject": ticket.subject,
        "status": ticket.status,
        "priority": 1,
        "source": 2,
        "requester_id": 6007738334,
        "tags": [],
        "created_at": ticket.created_at.strftime(_TIME_LAYOUT),
        "updated_at": ticket.updated_at.strftime(_TIME_LAYOUT),
    }


def _json(status: int, value: Any, headers: dict[str, str]) -> httpx.Response:
    return httpx.Response(status, headers=headers, content=json.dumps(value).encode())
