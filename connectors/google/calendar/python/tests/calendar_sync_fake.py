# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""One Google Calendar's events.list full and incremental sync, served through httpx.MockTransport.

A port of the Go connector's internal calendarsyncfake: sync tokens are "sync-<sequence>", and every
insert, update, or cancel advances the sequence, as Google's documented sync protocol behaves.
"""

import json
import threading
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from urllib.parse import quote, unquote

import httpx

_FORBIDDEN_WITH_SYNC_TOKEN = (
    "iCalUID orderBy privateExtendedProperty q sharedExtendedProperty timeMin timeMax updatedMin".split()
)
_BASE_TIME = datetime(2026, 3, 2, 9, 0, tzinfo=UTC)


@dataclass(frozen=True, slots=True)
class RecordedRequest:
    """One recorded request with the status the fake answered."""

    path: str
    query: str
    status: int


@dataclass(slots=True)
class _StoredEvent:
    event_id: str
    summary: str
    status: str
    created_at: int
    sequence: int

    def resource(self) -> dict[str, object]:
        updated = _rfc3339(self.sequence)
        if self.status == "cancelled":
            return {
                "id": self.event_id,
                "status": "cancelled",
                "etag": f'"{self.sequence}"',
                "updated": updated,
            }
        return {
            "id": self.event_id,
            "status": "confirmed",
            "summary": self.summary,
            "etag": f'"{self.sequence}"',
            "created": _rfc3339(self.created_at),
            "updated": updated,
            "start": {"dateTime": "2026-03-10T10:00:00Z"},
            "end": {"dateTime": "2026-03-10T11:00:00Z"},
        }


def _rfc3339(minutes: int) -> str:
    return (_BASE_TIME + timedelta(minutes=minutes)).strftime("%Y-%m-%dT%H:%M:%SZ")


def _google_error(code: int, reason: str) -> dict[str, object]:
    return {"error": {"code": code, "errors": [{"reason": reason}]}}


class CalendarSyncFake:
    """A stateful single-calendar fake; safe to use from the test and the transport at once."""

    def __init__(self, calendar_id: str) -> None:
        self.calendar_id = calendar_id
        self._lock = threading.Lock()
        self._sequence = 0
        self._events: dict[str, _StoredEvent] = {}
        self._oldest_valid_token = 0
        self._throttled = 0
        self._throttle_status = 0
        self._throttle_reason = ""
        self._retry_after = ""
        self._requests: list[RecordedRequest] = []

    def transport(self) -> httpx.MockTransport:
        """Return a transport that serves this calendar."""
        return httpx.MockTransport(self.handle)

    def insert(self, event_id: str, summary: str) -> None:
        """Create a confirmed event."""
        with self._lock:
            self._sequence += 1
            self._events[event_id] = _StoredEvent(
                event_id, summary, "confirmed", self._sequence, self._sequence
            )

    def update(self, event_id: str, summary: str) -> None:
        """Change an existing event's summary."""
        with self._lock:
            self._sequence += 1
            event = self._events[event_id]
            event.summary, event.sequence = summary, self._sequence

    def cancel(self, event_id: str) -> None:
        """Delete an existing event; incremental sync then reports it with status cancelled."""
        with self._lock:
            self._sequence += 1
            event = self._events[event_id]
            event.status, event.sequence = "cancelled", self._sequence

    def expire_sync_tokens(self) -> None:
        """Make every sync token issued so far answer 410 Gone."""
        with self._lock:
            self._sequence += 1
            self._oldest_valid_token = self._sequence

    def rate_limit_next_requests(self, count: int, retry_after: str) -> None:
        """Answer the next count requests with 429 and the given Retry-After header value."""
        self.throttle_next_requests(count, 429, "rateLimitExceeded", retry_after)

    def throttle_next_requests(self, count: int, status: int, reason: str, retry_after: str) -> None:
        """Answer the next count requests with status, a Google error reason, and Retry-After."""
        with self._lock:
            self._throttled, self._throttle_status = count, status
            self._throttle_reason, self._retry_after = reason, retry_after

    def requests(self) -> list[RecordedRequest]:
        """Return the recorded requests in arrival order."""
        with self._lock:
            return list(self._requests)

    def sync_token(self) -> str:
        """Return the token a full sync would issue now."""
        with self._lock:
            return f"sync-{self._sequence}"

    def handle(self, request: httpx.Request) -> httpx.Response:
        """Serve one events.list or events.get request."""
        with self._lock:
            status, body = self._answer(request)
            self._requests.append(RecordedRequest(request.url.path, request.url.query.decode(), status))
            headers = {"Content-Type": "application/json"}
            if status == self._throttle_status and self._retry_after:
                headers["Retry-After"] = self._retry_after
            return httpx.Response(status, headers=headers, content=json.dumps(body).encode())

    def _answer(self, request: httpx.Request) -> tuple[int, dict[str, object]]:
        params = request.url.params
        raw_path = request.url.raw_path.split(b"?", 1)[0].decode()
        events_path = "/calendars/" + quote(self.calendar_id, safe="-._~$&+=:@") + "/events"
        if (
            request.method != "GET"
            or not raw_path.endswith(events_path)
            and events_path + "/" not in raw_path
        ):
            return 404, _google_error(404, "notFound")
        if not request.headers.get("Authorization"):
            return 401, _google_error(401, "authError")
        if self._throttled > 0:
            self._throttled -= 1
            return self._throttle_status, _google_error(self._throttle_status, self._throttle_reason)
        if events_path + "/" in raw_path:
            event = self._events.get(unquote(raw_path.rsplit("/", 1)[1]))
            return (200, event.resource()) if event else (404, _google_error(404, "notFound"))
        after = -1
        token = params.get("syncToken", "")
        if token:
            if (
                any(name in params for name in _FORBIDDEN_WITH_SYNC_TOKEN)
                or params.get("showDeleted") == "false"
            ):
                return 400, _google_error(400, "invalid")
            if not token.startswith("sync-") or not token[5:].isdigit():
                return 400, _google_error(400, "invalid")
            if int(token[5:]) < self._oldest_valid_token:
                return 410, _google_error(410, "fullSyncRequired")
            after = int(token[5:])
        page_size = int(params.get("maxResults", "250") or "250")
        offset, snapshot = 0, self._sequence
        page_token = params.get("pageToken", "")
        if page_token:
            _, offset_text, snapshot_text = page_token.split("-")
            offset, snapshot = int(offset_text), int(snapshot_text)
        items = sorted(
            (
                event
                for event in self._events.values()
                if event.sequence <= snapshot
                and not (after < 0 and event.status == "cancelled")
                and not (after >= 0 and event.sequence <= after)
            ),
            key=lambda event: event.sequence,
        )
        end = min(offset + page_size, len(items))
        body: dict[str, object] = {
            "kind": "calendar#events",
            "items": [e.resource() for e in items[offset:end]],
        }
        if end < len(items):
            body["nextPageToken"] = f"page-{end}-{snapshot}"
        else:
            body["nextSyncToken"] = f"sync-{snapshot}"
        return 200, body
