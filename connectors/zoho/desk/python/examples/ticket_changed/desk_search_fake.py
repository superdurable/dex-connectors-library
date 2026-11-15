# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""A stateful Zoho Desk stand-in for the ticket search and ticket read endpoints the example uses.

It mirrors the Go connector's internal/desksearchfake: modifiedTimeRange filtering, ascending modifiedTime
order, from and limit paging within Zoho Desk's documented bounds, a Date header from the fake's clock, an
indexing delay, a ticket listed twice, and injected 429 and 422 responses.
"""

import json
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from email.utils import format_datetime
from typing import Any

import httpx

ACCESS_TOKEN = "1000.zohoDeskTriggerAccess0123456789"
"""The only bearer token the fake accepts."""
ORGANIZATION_ID = "2389290"
"""The only orgId header the fake accepts."""
BASE_URL = "https://desk.zoho.test/api/v1"
"""The API base URL the fake answers at."""

_MAXIMUM_SEARCH_LIMIT = 100
_MAXIMUM_SEARCH_FROM = 999


@dataclass
class _Ticket:
    id: str
    subject: str
    status: str
    department_id: str
    created_at: datetime
    modified_at: datetime

    def wire(self) -> dict[str, Any]:
        return {
            "id": self.id,
            "ticketNumber": self.id.removeprefix("18920000002"),
            "subject": self.subject,
            "description": f"<div>{self.subject}</div>",
            "status": self.status,
            "statusType": self.status if self.status in ("Closed", "On Hold") else "Open",
            "priority": "Medium",
            "channel": "Email",
            "departmentId": self.department_id,
            "contactId": "1892000000042032",
            "email": "jane@example.test",
            "createdTime": _format(self.created_at),
            "modifiedTime": _format(self.modified_at),
        }


@dataclass(frozen=True)
class Search:
    """One received search request."""

    modified_time_range: str
    """The modifiedTimeRange query parameter."""
    from_: int
    """The from query parameter."""
    limit: int
    """The limit query parameter."""
    department_id: str
    """The departmentId query parameter, empty when absent."""


def _format(moment: datetime) -> str:
    return moment.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%S.") + f"{moment.microsecond // 1000:03d}Z"


def _parse(value: str) -> datetime:
    return datetime.strptime(value, "%Y-%m-%dT%H:%M:%S.%fZ").replace(tzinfo=UTC)


def _truncate_to_millisecond(moment: datetime) -> datetime:
    return moment.replace(microsecond=moment.microsecond // 1000 * 1000)


class DeskSearchFake:
    """A Zoho Desk stand-in whose clock follows the wall clock, or a fixed start moved by `advance`."""

    def __init__(self, start: datetime | None = None) -> None:
        """Start the fake at start, or on the wall clock when start is None."""
        self._clock = start
        self._next_id = 1892000000200000
        self._tickets: dict[str, _Ticket] = {}
        self._hidden: set[str] = set()
        self._duplicates: set[str] = set()
        self._rate_limits = 0
        self._retry_after = ""
        self._rejected_ranges = 0
        self.searches: list[Search] = []
        """Every search received, in order."""

    def now(self) -> datetime:
        """Return the fake's clock, which every response also sends in its Date header."""
        if self._clock is None:
            return _truncate_to_millisecond(datetime.now(UTC))
        return self._clock

    def advance(self, duration: timedelta) -> None:
        """Move a fixed clock forward."""
        assert self._clock is not None, "a wall-clock fake cannot advance"
        self._clock += duration

    def transport(self) -> httpx.MockTransport:
        """Return an httpx transport that answers every request from this fake."""
        return httpx.MockTransport(self._handle)

    def create(self, subject: str, department_id: str) -> str:
        """Add a ticket created and modified now and return its ID."""
        self._next_id += 1
        ticket_id = str(self._next_id)
        now = self.now()
        self._tickets[ticket_id] = _Ticket(ticket_id, subject, "Open", department_id, now, now)
        return ticket_id

    def update(self, ticket_id: str, status: str) -> None:
        """Set a ticket's status and move its modified time to now."""
        self._tickets[ticket_id].status = status
        self._tickets[ticket_id].modified_at = self.now()

    def hide_from_search(self, ticket_id: str, is_hidden: bool) -> None:
        """Model the indexing delay: the ticket is readable but absent from search until shown."""
        if is_hidden:
            self._hidden.add(ticket_id)
        else:
            self._hidden.discard(ticket_id)

    def list_twice(self, ticket_id: str) -> None:
        """Model a search index that lists the ticket a second time on the page holding it."""
        self._duplicates.add(ticket_id)

    def rate_limit_next(self, count: int, retry_after: str) -> None:
        """Answer the next count searches with 429 and retry_after, which may be empty."""
        self._rate_limits, self._retry_after = count, retry_after

    def reject_next_ranges(self, count: int) -> None:
        """Answer the next count searches with 422 naming modifiedTimeRange."""
        self._rejected_ranges = count

    def _handle(self, request: httpx.Request) -> httpx.Response:
        # Zoho Desk, like any HTTP server, dates each response with its own clock.
        headers = {"Date": format_datetime(self.now().replace(microsecond=0), usegmt=True)}
        if request.headers.get("Authorization") != "Zoho-oauthtoken " + ACCESS_TOKEN:
            return _json(401, {"errorCode": "INVALID_OAUTH"}, headers)
        if request.headers.get("orgId") != ORGANIZATION_ID:
            return _json(403, {"errorCode": "OAUTH_ORG_MISMATCH"}, headers)
        path = request.url.path.removeprefix("/api/v1")
        if request.method == "GET" and path == "/tickets/search":
            return self._search(request, headers)
        if request.method == "GET" and path.startswith("/tickets/"):
            return self._read(path.removeprefix("/tickets/").split("/"), headers)
        return _json(404, {"errorCode": "URL_NOT_FOUND"}, headers)

    def _search(self, request: httpx.Request, headers: dict[str, str]) -> httpx.Response:
        query = request.url.params
        try:
            from_, limit = int(query.get("from", "")), int(query.get("limit", ""))
        except ValueError:
            from_, limit = -1, -1
        self.searches.append(
            Search(query.get("modifiedTimeRange", ""), from_, limit, query.get("departmentId", ""))
        )
        if self._rate_limits > 0:
            self._rate_limits -= 1
            if self._retry_after:
                headers["Retry-After"] = self._retry_after
            return _json(429, {"errorCode": "THRESHOLD_EXCEEDED"}, headers)
        lower: datetime | None = None
        upper: datetime | None = None
        try:
            lower_text, upper_text = query.get("modifiedTimeRange", "").split(",", 1)
            lower, upper = _parse(lower_text), _parse(upper_text)
        except ValueError:
            pass
        if self._rejected_ranges > 0 or lower is None or upper is None:
            self._rejected_ranges = max(0, self._rejected_ranges - 1)
            return _json(422, _invalid("/modifiedTimeRange"), headers)
        if not 0 <= from_ <= _MAXIMUM_SEARCH_FROM or not 1 <= limit <= _MAXIMUM_SEARCH_LIMIT:
            return _json(422, _invalid("/from"), headers)
        if query.get("sortBy") != "modifiedTime":
            return _json(422, _invalid("/from"), headers)
        department_id = query.get("departmentId", "")
        matches = sorted(
            (
                ticket
                for ticket in self._tickets.values()
                if ticket.id not in self._hidden
                and lower <= ticket.modified_at <= upper
                and (not department_id or ticket.department_id == department_id)
            ),
            key=lambda ticket: (ticket.modified_at, ticket.id),
        )
        if from_ >= len(matches):
            return httpx.Response(204, headers=headers)
        data: list[dict[str, Any]] = []
        for ticket in matches[from_ : from_ + limit]:
            data.append(ticket.wire())
            if ticket.id in self._duplicates:
                data.append(ticket.wire())
        # A listed-twice ticket takes a page slot, as an index entry would.
        return _json(200, {"data": data[:limit], "count": str(len(matches))}, headers)

    def _read(self, segments: list[str], headers: dict[str, str]) -> httpx.Response:
        ticket = self._tickets.get(segments[0])
        if ticket is None:
            return _json(404, {"errorCode": "URL_NOT_FOUND"}, headers)
        if len(segments) == 1:
            return _json(200, ticket.wire(), headers)
        if len(segments) == 2 and segments[1] in ("threads", "comments"):
            return _json(200, {"data": []}, headers)
        return _json(404, {"errorCode": "URL_NOT_FOUND"}, headers)


def _invalid(field_name: str) -> dict[str, Any]:
    return {
        "errorCode": "UNPROCESSABLE_ENTITY",
        "errors": [{"fieldName": field_name, "errorType": "invalid"}],
    }


def _json(status: int, value: object, headers: dict[str, str]) -> httpx.Response:
    return httpx.Response(
        status, headers={**headers, "Content-Type": "application/json"}, content=json.dumps(value).encode()
    )
