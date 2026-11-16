# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""A stateful Re:amaze conversation store served through httpx for conversationCreated tests.

It serves the conversation list, conversation read, and message list that the conversationCreated Trigger
and getConversation use, over HTTP Basic, with response bytes identical to the Go connector's
reamazefake, so Go-recorded fetches replay against it exactly.
"""

import base64
import threading
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any
from urllib.parse import parse_qs

import httpx
from dex_connectors.sdk.projectconfig import encode_go_json_value

PAGE_SIZE = 30
"""The number of conversations Re:amaze lists per page."""


@dataclass(frozen=True, slots=True)
class StoredConversation:
    """One stored conversation."""

    slug: str
    subject: str
    channel: str
    status: int
    created_at: datetime
    body: str


def _go_time_milliseconds(moment: datetime) -> str:
    """Format as Go's "2006-01-02T15:04:05.000Z07:00" layout in UTC."""
    utc = moment.astimezone(UTC)
    return utc.strftime("%Y-%m-%dT%H:%M:%S") + f".{utc.microsecond // 1000:03d}Z"


def _go_time_rfc3339_nano(moment: datetime) -> str:
    """Format as Go's time.RFC3339Nano in UTC, which trims trailing fractional zeros."""
    utc = moment.astimezone(UTC)
    fraction = f"{utc.microsecond:06d}".rstrip("0")
    return utc.strftime("%Y-%m-%dT%H:%M:%S") + (f".{fraction}" if fraction else "") + "Z"


def _json_response(value: Any) -> httpx.Response:
    # Go's json.Encoder sorts map keys, escapes HTML characters, and ends with a newline.
    return httpx.Response(
        200, headers={"Content-Type": "application/json"}, content=encode_go_json_value(value) + b"\n"
    )


def _error_response(status: int, body: str, headers: dict[str, str] | None = None) -> httpx.Response:
    # As Go's http.Error writes it.
    response_headers = {"Content-Type": "text/plain; charset=utf-8", "X-Content-Type-Options": "nosniff"}
    response_headers.update(headers or {})
    return httpx.Response(status, headers=response_headers, content=(body + "\n").encode())


def _conversation_wire(conversation: StoredConversation) -> dict[str, Any]:
    channel = conversation.channel or "support"
    return {
        "slug": conversation.slug,
        "subject": conversation.subject,
        "status": conversation.status,
        "created_at": _go_time_milliseconds(conversation.created_at),
        "tag_list": [],
        "message": {"body": conversation.body},
        "author": {"name": "Jane Smith", "email": "jane@acme.example.com"},
        "category": {"name": channel[:1].upper() + channel[1:], "slug": channel, "channel": 1},
    }


class ReamazeConversationFake:
    """A Re:amaze stand-in that accepts one email and API token over HTTP Basic; safe across threads."""

    base_url = "https://acme.reamaze.io/api/v1"
    """The base URL to pass as the client's `api_base_url`."""

    def __init__(self, email: str, token: str) -> None:
        """Start an empty store for the email and token."""
        self._authorization = "Basic " + base64.b64encode(f"{email}:{token}".encode()).decode()
        self._lock = threading.Lock()
        self._stored: list[StoredConversation] = []
        self._hidden: set[str] = set()
        self._is_ascending = False
        self._rate_limits: list[str] = []
        self._list_requests = 0
        self._listed_pages: list[int] = []
        self._listed_queries: list[dict[str, str]] = []

    def transport(self) -> httpx.MockTransport:
        """Return a transport that serves this store."""
        return httpx.MockTransport(self._serve)

    def insert(
        self,
        slug: str,
        subject: str,
        *,
        channel: str = "",
        status: int = 0,
        created_at: datetime | None = None,
        body: str = "",
    ) -> None:
        """Store a conversation; without created_at it is now, truncated to milliseconds."""
        if created_at is None:
            now = datetime.now(UTC)
            created_at = now.replace(microsecond=now.microsecond // 1000 * 1000)
        with self._lock:
            self._stored.append(
                StoredConversation(
                    slug, subject, channel, status, created_at, body or "Opening message of " + subject
                )
            )

    def hide(self, slug: str) -> None:
        """Keep a stored conversation out of lists until `reveal`, as Re:amaze's delayed listing does."""
        with self._lock:
            self._hidden.add(slug)

    def reveal(self, slug: str) -> None:
        """List a hidden conversation."""
        with self._lock:
            self._hidden.discard(slug)

    def list_oldest_first(self) -> None:
        """Switch the list order to ascending creation time."""
        with self._lock:
            self._is_ascending = True

    def rate_limit_next_list(self, retry_after: str) -> None:
        """Answer the next list request with HTTP 429 and retry_after, which may be empty."""
        with self._lock:
            self._rate_limits.append(retry_after)

    def list_request_count(self) -> int:
        """Return the number of list requests, including rate-limited ones."""
        with self._lock:
            return self._list_requests

    def listed_pages(self) -> list[int]:
        """Return the page parameter of every answered list request, in order."""
        with self._lock:
            return list(self._listed_pages)

    def listed_queries(self) -> list[dict[str, str]]:
        """Return the query of every answered list request, in order."""
        with self._lock:
            return [dict(query) for query in self._listed_queries]

    def _serve(self, request: httpx.Request) -> httpx.Response:
        if request.headers.get("Authorization") != self._authorization:
            return _error_response(401, '{"error":"unauthorized"}')
        path = request.url.path.removeprefix("/api/v1")
        if request.method != "GET":
            return _error_response(405, "method not allowed")
        if path == "/conversations":
            query = {name: values[0] for name, values in parse_qs(request.url.query.decode()).items()}
            return self._list(query)
        if path.startswith("/conversations/") and path.endswith("/messages"):
            return self._messages(path.removeprefix("/conversations/").removesuffix("/messages"))
        if path.startswith("/conversations/"):
            conversation = self._find(path.removeprefix("/conversations/"))
            if conversation is None:
                return _error_response(404, '{"error":"not found"}')
            return _json_response(_conversation_wire(conversation))
        return _error_response(404, "404 page not found")

    def _list(self, query: dict[str, str]) -> httpx.Response:
        """List by creation time, newest first unless ascending; filter all includes archived (4)."""
        with self._lock:
            self._list_requests += 1
            if self._rate_limits:
                retry_after = self._rate_limits.pop(0)
                headers = {"Retry-After": retry_after} if retry_after else {}
                return _error_response(429, '{"error":"rate limited"}', headers)
            try:
                page = int(query.get("page", ""))
            except ValueError:
                page = 1
            page = max(page, 1)
            self._listed_pages.append(page)
            self._listed_queries.append(query)
            filter_name, category = query.get("filter", ""), query.get("category", "")
            matching = [
                conversation
                for conversation in self._stored
                if conversation.slug not in self._hidden
                and not (conversation.status == 4 and filter_name != "all")
                and not (category and conversation.channel != category)
            ]
            # A stable sort keeps insertion order among equal times in both directions, as Go's does.
            matching.sort(key=lambda conversation: conversation.created_at, reverse=not self._is_ascending)
            page_count = (len(matching) + PAGE_SIZE - 1) // PAGE_SIZE
            start = min((page - 1) * PAGE_SIZE, len(matching))
            listed = matching[start : start + PAGE_SIZE]
            entries = [_conversation_wire(conversation) for conversation in listed]
            return _json_response(
                {
                    "page_size": PAGE_SIZE,
                    "page_count": page_count,
                    "total_count": len(matching),
                    "conversations": entries,
                }
            )

    def _messages(self, slug: str) -> httpx.Response:
        conversation = self._find(slug)
        if conversation is None:
            return _error_response(404, '{"error":"not found"}')
        message = {
            "body": conversation.body,
            "visibility": 0,
            "origin": 1,
            "created_at": _go_time_rfc3339_nano(conversation.created_at),
            "user": {"name": "Jane Smith", "email": "jane@acme.example.com"},
        }
        return _json_response(
            {"page_size": PAGE_SIZE, "page_count": 1, "total_count": 1, "messages": [message]}
        )

    def _find(self, slug: str) -> StoredConversation | None:
        with self._lock:
            return next((conversation for conversation in self._stored if conversation.slug == slug), None)
