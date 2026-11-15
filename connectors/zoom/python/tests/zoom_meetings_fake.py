# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""A single-user Zoom fake serving GET /users/me/meetings and GET /meetings/{id} for meetingChanged tests.

It mirrors the Go connector's internal/zoommeetingsfake: page_size up to 300, an opaque next_page_token,
and meetings in creation order. Serve it through `httpx.MockTransport(fake.handle)`.
"""

from dataclasses import dataclass
from datetime import UTC, datetime, timedelta

import httpx

ACCESS_TOKEN = "zoom-trigger-token"
_BASE_TIME = datetime(2036, 2, 23, 17, 0, tzinfo=UTC)


def _rfc3339(instant: datetime) -> str:
    return instant.strftime("%Y-%m-%dT%H:%M:%SZ")


@dataclass(slots=True)
class _Meeting:
    meeting_id: int
    topic: str
    start_time: datetime
    duration: int
    created_order: int


@dataclass(frozen=True, slots=True)
class RecordedRequest:
    """One recorded request and the status the fake answered."""

    path: str
    query: dict[str, str]
    status: int


class ZoomMeetingsFake:
    """A stateful fake of one Zoom user's scheduled meetings."""

    def __init__(self, host_id: str) -> None:
        self.host_id = host_id
        self._created = 0
        self._meetings: dict[int, _Meeting] = {}
        self._deleted: dict[int, _Meeting] = {}
        self._rejected_page_tokens = 0
        self._rate_limited = 0
        self._rate_limit_is_daily = False
        self._retry_after = ""
        self._issued_page_tokens: dict[str, int] = {}
        self.requests: list[RecordedRequest] = []

    def schedule(self, meeting_id: int, topic: str, days: int) -> None:
        """Create a 30-minute meeting starting days after the base time."""
        self._created += 1
        start = _BASE_TIME + timedelta(days=days)
        self._meetings[meeting_id] = _Meeting(meeting_id, topic, start, 30, self._created)

    def reschedule(self, meeting_id: int, hours: int) -> None:
        """Move an existing meeting's start by hours."""
        self._meetings[meeting_id].start_time += timedelta(hours=hours)

    def rename(self, meeting_id: int, topic: str) -> None:
        """Change an existing meeting's topic."""
        self._meetings[meeting_id].topic = topic

    def delete(self, meeting_id: int) -> None:
        """Move a meeting to Recently Deleted, where `recover` can restore it."""
        self._deleted[meeting_id] = self._meetings.pop(meeting_id)

    def recover(self, meeting_id: int) -> None:
        """Restore a deleted meeting unchanged."""
        self._meetings[meeting_id] = self._deleted.pop(meeting_id)

    def reject_next_page_tokens(self, count: int) -> None:
        """Answer the next count requests carrying a page token with 400, as after Zoom's token expiry."""
        self._rejected_page_tokens = count

    def rate_limit_next(self, count: int, is_daily: bool, retry_after: str) -> None:
        """Answer the next count listings with 429, with Zoom's daily message when is_daily."""
        self._rate_limited, self._rate_limit_is_daily, self._retry_after = count, is_daily, retry_after

    def listing_request_count(self) -> int:
        """Count listing requests."""
        return sum(1 for request in self.requests if request.path.endswith("/users/me/meetings"))

    def handle(self, request: httpx.Request) -> httpx.Response:
        """Answer one request."""
        headers: dict[str, str] = {}
        status, body = self._answer(request, headers)
        path = request.url.path.removeprefix("/v2")
        self.requests.append(RecordedRequest(path, dict(request.url.params), status))
        return httpx.Response(status, json=body, headers=headers)

    def _answer(self, request: httpx.Request, headers: dict[str, str]) -> tuple[int, object]:
        if request.method != "GET" or request.headers.get("Authorization") != "Bearer " + ACCESS_TOKEN:
            return 401, {"code": 124, "message": "Invalid access token."}
        path = request.url.path.removeprefix("/v2")
        if path.startswith("/meetings/"):
            return self._answer_meeting(path.removeprefix("/meetings/"))
        if path != "/users/me/meetings":
            return 404, {"code": 1001, "message": "User does not exist."}
        if self._rate_limited > 0:
            self._rate_limited -= 1
            if self._retry_after:
                headers["Retry-After"] = self._retry_after
            message = "You have reached the maximum per-second rate limit for this API. Try again later."
            if self._rate_limit_is_daily:
                message = (
                    "You have reached the maximum daily rate limit for this API. Refer to the response "
                    "header for details on when you can make another call."
                )
            return 429, {"code": 429, "message": message}
        return self._answer_listing(dict(request.url.params))

    def _answer_listing(self, query: dict[str, str]) -> tuple[int, object]:
        invalid_token = {"code": 300, "message": "The next page token is invalid or expired."}
        if query.get("type") != "scheduled":
            return 400, {"code": 300, "message": "Unexpected type."}
        page_size = int(query.get("page_size", "0"))
        if not 1 <= page_size <= 300:
            return 400, {"code": 300, "message": "Invalid page_size."}
        offset = 0
        token = query.get("next_page_token", "")
        if token:
            if self._rejected_page_tokens > 0:
                self._rejected_page_tokens -= 1
                return 400, invalid_token
            if token not in self._issued_page_tokens:
                return 400, invalid_token
            offset = self._issued_page_tokens[token]
        ordered = sorted(self._meetings.values(), key=lambda meeting: meeting.created_order)
        end = min(offset + page_size, len(ordered))
        items = [self._resource(meeting) for meeting in ordered[min(offset, end) : end]]
        next_page_token = ""
        if end < len(ordered):
            next_page_token = f"page-{len(self._issued_page_tokens) + 1}-{end}"
            self._issued_page_tokens[next_page_token] = end
        return 200, {
            "next_page_token": next_page_token,
            "page_size": page_size,
            "total_records": len(ordered),
            "meetings": items,
        }

    def _answer_meeting(self, raw_id: str) -> tuple[int, object]:
        stored = self._meetings.get(int(raw_id)) if raw_id.isdecimal() else None
        if stored is None:
            return 404, {"code": 3001, "message": "Meeting does not exist."}
        resource = self._resource(stored)
        resource["settings"] = {"waiting_room": True}
        return 200, resource

    def _resource(self, stored: _Meeting) -> dict[str, object]:
        return {
            "id": stored.meeting_id,
            "uuid": f"uuid-{stored.meeting_id}==",
            "host_id": self.host_id,
            "topic": stored.topic,
            "type": 2,
            "start_time": _rfc3339(stored.start_time),
            "duration": stored.duration,
            "timezone": "America/Los_Angeles",
            "join_url": f"https://us05web.zoom.us/j/{stored.meeting_id}",
            "created_at": _rfc3339(_BASE_TIME - timedelta(hours=stored.created_order)),
        }
