# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""One monday.com board served over GraphQL for itemChanged tests, ported from Go's mondayboardfake.

It answers items_page ordered by __last_updated__, next_items_page with single-use cursors, and items
by ID. Every change advances the board clock by one second unless the test sets the clock itself.
"""

import json
import threading
from dataclasses import dataclass, field, replace
from datetime import UTC, datetime, timedelta
from typing import Any

import httpx

TOKEN = "monday-fake-token"
BASE_TIME = datetime(2026, 3, 2, 9, 0, tzinfo=UTC)
_STATUS_INDEX = {"Working on it": 0, "Done": 1, "Stuck": 2}


@dataclass(frozen=True, slots=True)
class FakeItem:
    """One active item on the fake board."""

    id: str
    name: str
    status: str
    created_at: datetime
    updated_at: datetime
    group_id: str = "topics"


@dataclass(frozen=True, slots=True)
class RecordedRequest:
    """One GraphQL request the fake answered."""

    operation: str
    variables: dict[str, Any]
    status: int


@dataclass
class MondayBoardFake:
    """A stateful single-board fake, safe to call from several tasks."""

    board_id: str
    clock: datetime = BASE_TIME
    items: dict[str, FakeItem] = field(default_factory=dict)
    requests: list[RecordedRequest] = field(default_factory=list)
    rate_limited_calls: int = 0
    retry_after_seconds: int = 0
    daily_limit_calls: int = 0
    _cursors: dict[str, list[FakeItem]] = field(default_factory=dict)
    _next_cursor: int = 0
    _lock: threading.Lock = field(default_factory=threading.Lock)

    def create(self, item_id: str, name: str) -> None:
        """Add an active item with status Working on it."""
        with self._lock:
            now = self._tick()
            self.items[item_id] = FakeItem(item_id, name, "Working on it", now, now)

    def set_status(self, item_id: str, status: str) -> None:
        """Change an item's status label and move its last-updated time."""
        with self._lock:
            self.items[item_id] = replace(self.items[item_id], status=status, updated_at=self._tick())

    def archive(self, item_id: str) -> None:
        """Remove an item from items_page, which lists only active items."""
        with self._lock:
            del self.items[item_id]

    def set_clock(self, at: datetime) -> None:
        """Fix the time of the next change, such as a second change within the same second."""
        with self._lock:
            self.clock = at - timedelta(seconds=1)

    def rate_limit_next_requests(self, count: int, retry_after_seconds: int) -> None:
        """Answer the next count requests with HTTP 429, Retry-After, and RATE_LIMIT_EXCEEDED."""
        with self._lock:
            self.rate_limited_calls, self.retry_after_seconds = count, retry_after_seconds

    def exhaust_daily_limit_for_next_requests(self, count: int) -> None:
        """Answer the next count requests with DAILY_LIMIT_EXCEEDED."""
        with self._lock:
            self.daily_limit_calls = count

    def updated_at(self, item_id: str) -> datetime:
        """Return an item's last-updated time."""
        with self._lock:
            return self.items[item_id].updated_at

    def handle(self, request: httpx.Request) -> httpx.Response:
        """Answer one GraphQL request, as an httpx.MockTransport handler."""
        try:
            body = json.loads(request.content)
            query, variables = body["query"], body.get("variables") or {}
        except (ValueError, KeyError, TypeError):
            query, variables = "", {}
        fields = query.split()
        operation = fields[1].split("(")[0] if len(fields) > 1 and fields[0] == "query" else ""
        with self._lock:
            headers: dict[str, str] = {}
            if request.method != "POST" or not query or not request.headers.get("API-Version"):
                status, payload = 400, _error("GRAPHQL_PARSE_FAILED")
            elif request.headers.get("Authorization") != TOKEN:
                status, payload = 401, _error("UNAUTHORIZED")
            elif self.rate_limited_calls > 0:
                self.rate_limited_calls -= 1
                headers["Retry-After"] = str(self.retry_after_seconds)
                status, payload = (
                    429,
                    _error("RATE_LIMIT_EXCEEDED", retry_in_seconds=self.retry_after_seconds),
                )
            elif self.daily_limit_calls > 0:
                self.daily_limit_calls -= 1
                status, payload = 200, _error("DAILY_LIMIT_EXCEEDED")
            elif operation == "ListBoardItems":
                status, payload = self._list_first_page(variables)
            elif operation == "ListNextBoardItems":
                status, payload = self._list_next_page(variables)
            elif operation == "GetItem":
                status, payload = 200, self._get_items(variables)
            else:
                status, payload = 200, _error("undefinedField")
            self.requests.append(RecordedRequest(operation, variables, status))
        return httpx.Response(status, json=payload, headers=headers)

    def _tick(self) -> datetime:
        self.clock += timedelta(seconds=1)
        return self.clock

    def _list_first_page(self, variables: dict[str, Any]) -> tuple[int, Any]:
        if variables.get("boardIds") != [self.board_id]:
            return 200, {"data": {"boards": []}}
        order = {"order_by": [{"column_id": "__last_updated__", "direction": "desc"}]}
        if variables.get("queryParams") != order:
            return 200, _error("InvalidArgumentException")
        snapshot = sorted(self.items.values(), key=lambda item: (item.updated_at, item.id), reverse=True)
        page = self._page(snapshot, _limit(variables))
        return 200, {"data": {"boards": [{"id": self.board_id, "items_page": page}]}}

    def _list_next_page(self, variables: dict[str, Any]) -> tuple[int, Any]:
        remaining = self._cursors.pop(str(variables.get("cursor")), None)
        if remaining is None:
            return 200, _error("InvalidArgumentException")
        return 200, {"data": {"next_items_page": self._page(remaining, _limit(variables))}}

    def _page(self, items: list[FakeItem], size: int) -> dict[str, Any]:
        cursor = None
        if len(items) > size:
            self._next_cursor += 1
            cursor = f"cursor-{self._next_cursor}"
            self._cursors[cursor] = items[size:]
        return {"cursor": cursor, "items": [self._resource(item) for item in items[:size]]}

    def _get_items(self, variables: dict[str, Any]) -> Any:
        found = [
            self.items[str(item_id)]
            for item_id in variables.get("itemIds") or []
            if str(item_id) in self.items
        ]
        return {"data": {"items": [self._resource(item) for item in found]}}

    def _resource(self, item: FakeItem) -> dict[str, Any]:
        return {
            "id": item.id,
            "name": item.name,
            "state": "active",
            "url": f"https://contoso.monday.com/boards/{self.board_id}/pulses/{item.id}",
            "created_at": _rfc3339(item.created_at),
            "updated_at": _rfc3339(item.updated_at),
            "creator_id": "48202303",
            "board": {"id": self.board_id},
            "group": {"id": item.group_id, "title": "Topics"},
            "column_values": [
                {
                    "id": "status",
                    "type": "status",
                    "text": item.status,
                    "value": json.dumps({"index": _STATUS_INDEX.get(item.status, 0)}, separators=(",", ":")),
                }
            ],
        }


def _rfc3339(instant: datetime) -> str:
    return instant.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%SZ")


def _limit(variables: dict[str, Any]) -> int:
    limit = variables.get("limit")
    return limit if isinstance(limit, int) and limit >= 1 else 25


def _error(code: str, **extensions: Any) -> dict[str, Any]:
    return {"errors": [{"message": code, "extensions": {"code": code, **extensions}}]}
