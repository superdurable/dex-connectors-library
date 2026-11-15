# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""A stateful in-process Zendesk account: the incremental ticket export, ticket reads, and comment reads.

It mirrors the Go connector's internal/zendeskexportfake. Every create, update, or delete is one change
with the next sequence number; the export lists each ticket once, at its latest change, and cursors are
"c-<epoch>-<sequence>", as Zendesk's export reports each changed ticket's current state.
"""

import base64
import json
import threading
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any

import httpx

EMAIL = "admin@acme.example.com"
"""The only email the fake accepts, as {email}/token over HTTP Basic."""
API_TOKEN = "fake-zendesk-api-token"
"""The only API token the fake accepts."""
API_BASE_URL = "https://acme.zendesk.com/api/v2"
"""The API base URL the fake's transport serves."""


@dataclass
class _StoredTicket:
    ticket_id: int
    subject: str
    status: str
    created_at: datetime
    updated_at: datetime
    sequence: int


@dataclass(frozen=True, slots=True)
class RecordedRequest:
    """One request the fake answered."""

    path: str
    """The path below the API base URL, such as /incremental/tickets/cursor."""
    query: dict[str, str]
    """The request query parameters."""
    status: int
    """The HTTP status the fake answered."""


def _format_time(moment: datetime) -> str:
    return moment.strftime("%Y-%m-%dT%H:%M:%SZ")


class ZendeskExportFake:
    """A Zendesk account fake that is safe for concurrent use.

    Each change is stamped one minute before now, as Zendesk serves a change only once it is a minute old,
    and at least two seconds after the previous change, so generated_timestamp values are distinct and an
    update is more than a second after creation; `tie_changes` stamps a burst in one second instead.
    """

    def __init__(self) -> None:
        """Start an empty account."""
        self._lock = threading.Lock()
        self._sequence = 0
        self._clock: datetime | None = None
        self._tickets: dict[int, _StoredTicket] = {}
        self._cursor_epoch = 0
        self._rate_limited = 0
        self._retry_after = ""
        self._is_agent_only = False
        self._maximum_per_page = 1000
        self._is_tying_changes = False
        self._requests: list[RecordedRequest] = []
        self._expected_authorization = (
            "Basic " + base64.b64encode(f"{EMAIL}/token:{API_TOKEN}".encode()).decode()
        )

    def transport(self) -> httpx.MockTransport:
        """Return a transport that serves API_BASE_URL from this account."""
        return httpx.MockTransport(self._serve)

    def create(self, ticket_id: int, subject: str) -> None:
        """Add a new ticket."""
        with self._lock:
            now = self._advance()
            self._tickets[ticket_id] = _StoredTicket(ticket_id, subject, "new", now, now, self._sequence)

    def update(self, ticket_id: int, status: str) -> None:
        """Change an existing ticket's status."""
        with self._lock:
            now = self._advance()
            ticket = self._tickets[ticket_id]
            ticket.status, ticket.updated_at, ticket.sequence = status, now, self._sequence

    def delete(self, ticket_id: int) -> None:
        """Delete a ticket; the export reports it with status deleted, and reads answer 404."""
        self.update(ticket_id, "deleted")

    def invalidate_cursors(self) -> None:
        """Make every cursor issued so far answer HTTP 422."""
        with self._lock:
            self._cursor_epoch += 1

    def rate_limit_next_requests(self, count: int, retry_after: str) -> None:
        """Answer the next count export requests with 429 and the given Retry-After value."""
        with self._lock:
            self._rate_limited, self._retry_after = count, retry_after

    def reject_as_agent(self, is_agent_only: bool) -> None:
        """Answer the export with 403, as Zendesk does for a user who is not an administrator."""
        with self._lock:
            self._is_agent_only = is_agent_only

    def limit_per_page(self, maximum_per_page: int) -> None:
        """Cap per_page, as a smaller provider page would."""
        with self._lock:
            self._maximum_per_page = maximum_per_page

    def tie_changes(self, is_tying: bool) -> None:
        """Stamp every following change in the same second as the previous one while is_tying is true."""
        with self._lock:
            self._is_tying_changes = is_tying

    def cursor(self) -> str:
        """Return the cursor of the latest change, the after_cursor a caught-up export reports."""
        with self._lock:
            return self._epoch_cursor(self._sequence)

    def requests(self) -> list[RecordedRequest]:
        """Return the recorded requests in arrival order."""
        with self._lock:
            return list(self._requests)

    def _advance(self) -> datetime:
        self._sequence += 1
        served_at = datetime.now(UTC).replace(microsecond=0) - timedelta(minutes=1)
        if self._is_tying_changes and self._clock is not None:
            return self._clock
        clock = (self._clock or datetime.min.replace(tzinfo=UTC)) + timedelta(seconds=2)
        self._clock = served_at if served_at > clock else clock
        return self._clock

    def _epoch_cursor(self, sequence: int) -> str:
        return f"c-{self._cursor_epoch}-{sequence}"

    def _serve(self, request: httpx.Request) -> httpx.Response:
        with self._lock:
            path = request.url.path.removeprefix("/api/v2")
            query = dict(request.url.params.multi_items())
            if request.headers.get("Authorization") != self._expected_authorization:
                return self._answer(path, query, 401, {"error": "Couldn't authenticate you"})
            if path == "/incremental/tickets/cursor":
                return self._serve_export(path, query, request.url.params.get_list("cursor"))
            parts = path.strip("/").split("/")
            if len(parts) in (2, 3) and parts[0] == "tickets" and parts[1].isdigit():
                ticket = self._tickets.get(int(parts[1]))
                if ticket is None or ticket.status == "deleted":
                    return self._answer(path, query, 404, {"error": "RecordNotFound"})
                if len(parts) == 2:
                    users = [
                        {
                            "id": 7701,
                            "name": "Jane Smith",
                            "email": "jane@acme.example.com",
                            "role": "end-user",
                        }
                    ]
                    return self._answer(path, query, 200, {"ticket": _ticket_json(ticket), "users": users})
                if parts[2] == "comments":
                    comment = {
                        "id": ticket.ticket_id * 10,
                        "author_id": 7701,
                        "public": True,
                        "plain_body": "Opened: " + ticket.subject,
                        "created_at": _format_time(ticket.created_at),
                    }
                    return self._answer(
                        path, query, 200, {"comments": [comment], "meta": {"has_more": False}}
                    )
            return self._answer(path, query, 404, {"error": "InvalidEndpoint"})

    def _serve_export(self, path: str, query: dict[str, str], cursors: list[str]) -> httpx.Response:
        if self._rate_limited > 0:
            self._rate_limited -= 1
            headers = {"Retry-After": self._retry_after} if self._retry_after else {}
            return self._answer(path, query, 429, {"error": "APIRateLimitExceeded"}, headers)
        if self._is_agent_only:
            return self._answer(path, query, 403, {"error": "Forbidden"})
        per_page = 1000
        if "per_page" in query:
            if not query["per_page"].isdigit() or not 1 <= int(query["per_page"]) <= 1000:
                return self._answer(path, query, 400, {"error": "InvalidPaginationParameter"})
            per_page = int(query["per_page"])
        per_page = min(per_page, self._maximum_per_page)
        is_cursor = len(cursors) == 1
        if is_cursor:
            epoch, _, sequence = cursors[0].removeprefix("c-").partition("-")
            if epoch != str(self._cursor_epoch) or not sequence.isdigit():
                return self._answer(path, query, 422, {"error": "InvalidPaginationParameter"})
            after = int(sequence)
        else:
            start_time = query.get("start_time", "")
            latest = int((datetime.now(UTC) - timedelta(minutes=1)).timestamp())
            # Zendesk serves no data for the most recent minute.
            if not start_time.lstrip("-").isdigit() or int(start_time) > latest:
                return self._answer(path, query, 400, {"error": "InvalidPaginationParameter"})
            after = int(start_time)
        is_excluding_deleted = query.get("exclude_deleted") == "true"
        matches = []
        for ticket in self._tickets.values():
            is_after = ticket.sequence > after if is_cursor else int(ticket.updated_at.timestamp()) >= after
            if is_after and not (is_excluding_deleted and ticket.status == "deleted"):
                matches.append(ticket)
        matches.sort(key=lambda ticket: ticket.sequence)
        next_sequence = self._sequence
        is_end = len(matches) <= per_page
        if not is_end:
            matches = matches[:per_page]
            next_sequence = matches[-1].sequence
        body = {
            "tickets": [_ticket_json(ticket) for ticket in matches],
            "after_cursor": self._epoch_cursor(next_sequence),
            "before_cursor": None,
            "end_of_stream": is_end,
        }
        return self._answer(path, query, 200, body)

    def _answer(
        self, path: str, query: dict[str, str], status: int, body: Any, headers: dict[str, str] | None = None
    ) -> httpx.Response:
        self._requests.append(RecordedRequest(path, query, status))
        return httpx.Response(
            status, headers={"Content-Type": "application/json", **(headers or {})}, content=json.dumps(body)
        )


def _ticket_json(ticket: _StoredTicket) -> dict[str, Any]:
    return {
        "id": ticket.ticket_id,
        "subject": ticket.subject,
        "description": "Opened: " + ticket.subject,
        "status": ticket.status,
        "requester_id": 7701,
        "tags": ["dex-fake"],
        "via": {"channel": "api"},
        "created_at": _format_time(ticket.created_at),
        "updated_at": _format_time(ticket.updated_at),
        "generated_timestamp": int(ticket.updated_at.timestamp()),
    }
