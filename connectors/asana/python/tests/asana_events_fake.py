# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""One Asana project's Events API stream and tasks, served through an httpx mock transport.

It mirrors the Go connector's internal/asanaeventsfake: sync tokens are "sync-<sequence>"; a request
without one, or with an expired one, answers 412 with a fresh token, and each page holds at most 100
events, as Asana documents.
"""

import json
import threading
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any

import httpx

ACCESS_TOKEN = "asana-fake-token"
ENDPOINT = "https://asana.fake.test/api/1.0"
_PAGE_SIZE = 100
_BASE_TIME = datetime(2026, 3, 2, 9, 0, 0, tzinfo=UTC)


@dataclass(frozen=True, slots=True)
class RecordedRequest:
    """One request the fake answered."""

    path: str
    query: dict[str, str]
    status: int


class AsanaEventsFake:
    """A stateful single-project fake that is safe to call from the Worker and test threads."""

    def __init__(self, project_id: str) -> None:
        self.project_id = project_id
        self._lock = threading.Lock()
        self._events: list[dict[str, Any]] = []
        self._tasks: dict[str, dict[str, Any]] = {}
        self._oldest_valid_token = 0
        self._rate_limited = 0
        self._retry_after = ""
        self.requests: list[RecordedRequest] = []

    def http_client(self) -> httpx.AsyncClient:
        """Return a client whose requests this fake answers; the caller closes it."""
        return httpx.AsyncClient(transport=httpx.MockTransport(self.handle))

    def add_task(self, task_id: str, name: str) -> None:
        """Create a task in the project and record its added event, with the project as parent."""
        with self._lock:
            self._tasks[task_id] = {"name": name, "completed": False}
            parent = {"gid": self.project_id, "resource_type": "project", "name": "Facilities"}
            self._append_event(task_id, "added", parent, None)

    def rename_task(self, task_id: str, name: str) -> None:
        """Change a task's name and record a changed event for the name field."""
        with self._lock:
            self._tasks[task_id]["name"] = name
            self._append_event(
                task_id, "changed", None, {"field": "name", "action": "changed", "new_value": name}
            )

    def complete_task(self, task_id: str) -> None:
        """Mark a task completed and record a changed event for the completed field."""
        with self._lock:
            self._tasks[task_id]["completed"] = True
            change = {"field": "completed", "action": "changed", "new_value": True}
            self._append_event(task_id, "changed", None, change)

    def delete_task(self, task_id: str) -> None:
        """Move a task to the trash and record a deleted event."""
        with self._lock:
            self._tasks.pop(task_id, None)
            self._append_event(task_id, "deleted", None, None)

    def add_story(self, story_id: str) -> None:
        """Record a comment story event, which a taskChanged Trigger must ignore."""
        with self._lock:
            self._events.append(
                {
                    "action": "added",
                    "created_at": self._event_time(),
                    "user": _user(),
                    "resource": {
                        "gid": story_id,
                        "resource_type": "story",
                        "resource_subtype": "comment_added",
                    },
                }
            )

    def expire_sync_tokens(self) -> None:
        """Make every token older than the latest event answer 412, as Asana does after 24 hours."""
        with self._lock:
            self._oldest_valid_token = len(self._events)

    def rate_limit_next(self, count: int, retry_after: str) -> None:
        """Answer the next count /events requests with 429 and the given Retry-After header."""
        with self._lock:
            self._rate_limited, self._retry_after = count, retry_after

    def sync_token(self) -> str:
        """Return the token that follows every event recorded so far."""
        with self._lock:
            return f"sync-{len(self._events)}"

    def handle(self, request: httpx.Request) -> httpx.Response:
        """Answer one request and record it."""
        with self._lock:
            status, body = self._route(request)
            self.requests.append(RecordedRequest(request.url.path, dict(request.url.params), status))
            headers = {"Content-Type": "application/json"}
            if status == 429 and self._retry_after:
                headers["Retry-After"] = self._retry_after
            return httpx.Response(status, headers=headers, content=json.dumps(body).encode())

    def _route(self, request: httpx.Request) -> tuple[int, Any]:
        if request.headers.get("Authorization") != f"Bearer {ACCESS_TOKEN}":
            return 401, _error_body("Not Authorized")
        if request.method != "GET":
            return 405, _error_body("method not allowed")
        path = request.url.path.removeprefix("/api/1.0")
        if path == "/events":
            return self._serve_events(request.url.params)
        if path.startswith("/tasks/"):
            return self._serve_task(path.removeprefix("/tasks/"))
        return 404, _error_body("Unknown object")

    def _serve_events(self, query: httpx.QueryParams) -> tuple[int, Any]:
        if query.get("resource") != self.project_id:
            return 404, _error_body("Unknown object")
        if self._rate_limited > 0:
            self._rate_limited -= 1
            return 429, _error_body("Rate limit exceeded")
        fresh = {
            "errors": [{"message": "Sync token invalid or too old."}],
            "sync": f"sync-{len(self._events)}",
        }
        sequence_text = query.get("sync", "")
        if not sequence_text.startswith("sync-") or not sequence_text[5:].isdecimal():
            return 412, fresh
        sequence = int(sequence_text[5:])
        if sequence < self._oldest_valid_token or sequence > len(self._events):
            return 412, fresh
        end = min(sequence + _PAGE_SIZE, len(self._events))
        return 200, {
            "data": self._events[sequence:end],
            "sync": f"sync-{end}",
            "has_more": end < len(self._events),
        }

    def _serve_task(self, task_id: str) -> tuple[int, Any]:
        task = self._tasks.get(task_id)
        if task is None:
            return 404, _error_body("Unknown object")
        stamp = _BASE_TIME.strftime("%Y-%m-%dT%H:%M:%S.000Z")
        return 200, {
            "data": {
                "gid": task_id,
                "name": task["name"],
                "resource_subtype": "default_task",
                "completed": task["completed"],
                "memberships": [{"project": {"gid": self.project_id, "name": "Facilities"}}],
                "created_at": stamp,
                "modified_at": stamp,
            }
        }

    def _append_event(
        self, task_id: str, action: str, parent: dict[str, Any] | None, change: dict[str, Any] | None
    ) -> None:
        event: dict[str, Any] = {
            "action": action,
            "created_at": self._event_time(),
            "user": _user(),
            "parent": parent,
            "resource": {
                "gid": task_id,
                "resource_type": "task",
                "resource_subtype": "default_task",
                "name": self._tasks.get(task_id, {}).get("name", ""),
            },
        }
        if change is not None:
            event["change"] = change
        self._events.append(event)

    def _event_time(self) -> str:
        return (_BASE_TIME + timedelta(seconds=len(self._events))).strftime("%Y-%m-%dT%H:%M:%SZ")


def _user() -> dict[str, str]:
    return {"gid": "1203000000000001", "resource_type": "user", "name": "Ada Lovelace"}


def _error_body(message: str) -> dict[str, Any]:
    return {"errors": [{"message": message}]}
