# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""One Notion data source's rows for databaseRowChanged tests, a port of the Go notionrowsfake package.

It answers the data source query with a last_edited_time on_or_after filter and timestamp sort, and page
reads. Start cursors are "rows-<offset>". Tests set every row's created and last edited time, so minute
rounding and shared timestamps are scripted, not guessed.
"""

import json
import threading
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any

import httpx

TOKEN = "ntn_fake_rows_token"
"""The only API token the fake accepts; it is test data, not a credential."""
ENDPOINT = "https://notion-rows.test"
_NOTION_VERSION = "2026-03-11"


@dataclass
class RecordedRequest:
    """One request the fake answered."""

    method: str
    path: str
    body: dict[str, Any] | None
    status: int


@dataclass
class _StoredRow:
    id: str
    title: str
    created: datetime
    last_edited: datetime
    is_in_trash: bool = False


class NotionRowsFake:
    """A stateful single-data-source fake that is safe for concurrent use."""

    def __init__(self, data_source_id: str, database_id: str) -> None:
        self.data_source_id = data_source_id
        self.database_id = database_id
        self.requests: list[RecordedRequest] = []
        self._lock = threading.Lock()
        self._rows: dict[str, _StoredRow] = {}
        self._hidden_rows: set[str] = set()
        self._rate_limited = 0
        self._retry_after = ""
        self._is_start_cursor_stale = False

    def http_client(self) -> httpx.AsyncClient:
        """Return a client whose requests this fake answers in process."""
        return httpx.AsyncClient(transport=httpx.MockTransport(self.handle))

    def create(self, row_id: str, title: str, created_at: datetime) -> None:
        """Add a row created and last edited at created_at."""
        with self._lock:
            self._rows[row_id] = _StoredRow(row_id, title, created_at, created_at)

    def edit(self, row_id: str, title: str, edited_at: datetime) -> None:
        """Retitle a row and set its last edited time."""
        with self._lock:
            row = self._rows[row_id]
            row.title, row.last_edited = title, edited_at

    def trash(self, row_id: str, edited_at: datetime) -> None:
        """Move a row to the trash, which the data source query no longer returns."""
        with self._lock:
            row = self._rows[row_id]
            row.is_in_trash, row.last_edited = True, edited_at

    def hide_from_queries(self, row_id: str) -> None:
        """Simulate Notion indexing a row late: queries omit it until reveal_to_queries."""
        with self._lock:
            self._hidden_rows.add(row_id)

    def reveal_to_queries(self, row_id: str) -> None:
        """Make a hidden row visible to queries again."""
        with self._lock:
            self._hidden_rows.discard(row_id)

    def rate_limit_next_requests(self, count: int, retry_after: str) -> None:
        """Answer the next count requests 429 rate_limited with Retry-After seconds."""
        with self._lock:
            self._rate_limited, self._retry_after = count, retry_after

    def expire_start_cursors(self) -> None:
        """Answer the next query that sends a start cursor 400 validation_error."""
        with self._lock:
            self._is_start_cursor_stale = True

    def handle(self, request: httpx.Request) -> httpx.Response:
        """Answer one request and record it."""
        with self._lock:
            recorded = RecordedRequest(request.method, request.url.path, None, 0)
            status, body = self._answer(request, recorded)
            recorded.status = status
            self.requests.append(recorded)
            headers = {"Content-Type": "application/json"}
            if status == 429:
                headers["Retry-After"] = self._retry_after
        return httpx.Response(status, headers=headers, content=body.encode())

    def _answer(self, request: httpx.Request, recorded: RecordedRequest) -> tuple[int, str]:
        if (
            request.headers.get("Authorization") != "Bearer " + TOKEN
            or request.headers.get("Notion-Version") != _NOTION_VERSION
        ):
            return 401, _notion_error(401, "unauthorized")
        if self._rate_limited > 0:
            self._rate_limited -= 1
            return 429, _notion_error(429, "rate_limited")
        query_path = f"/v1/data_sources/{self.data_source_id}/query"
        if request.method == "POST" and request.url.path == query_path:
            try:
                body = json.loads(request.content)
            except ValueError:
                return 400, _notion_error(400, "invalid_json")
            recorded.body = body
            return self._answer_query(body)
        if request.method == "POST" and request.url.path.endswith("/query"):
            return 404, _notion_error(404, "object_not_found")
        if request.method == "GET" and request.url.path.startswith("/v1/pages/"):
            row = self._rows.get(request.url.path.removeprefix("/v1/pages/"))
            if row is None:
                return 404, _notion_error(404, "object_not_found")
            return 200, self._page_resource(row)
        return 404, _notion_error(404, "object_not_found")

    def _answer_query(self, body: dict[str, Any]) -> tuple[int, str]:
        sorts = body.get("sorts") or []
        page_size = body.get("page_size", 0)
        if len(sorts) != 1 or sorts[0].get("timestamp") != "last_edited_time" or not 1 <= page_size <= 100:
            return 400, _notion_error(400, "validation_error")
        offset = 0
        start_cursor = body.get("start_cursor", "")
        if start_cursor:
            if self._is_start_cursor_stale or not start_cursor.startswith("rows-"):
                self._is_start_cursor_stale = False
                return 400, _notion_error(400, "validation_error")
            offset = int(start_cursor.removeprefix("rows-"))
        lower_bound = datetime.min.replace(tzinfo=UTC)
        if body.get("filter") is not None:
            lower_bound = datetime.fromisoformat(body["filter"]["last_edited_time"]["on_or_after"])
        matched = [
            row
            for row in self._rows.values()
            if not row.is_in_trash and row.id not in self._hidden_rows and row.last_edited >= lower_bound
        ]
        is_descending = sorts[0].get("direction") == "descending"
        matched.sort(key=lambda row: row.id)
        matched.sort(key=lambda row: row.last_edited, reverse=is_descending)
        end = min(offset + page_size, len(matched))
        offset = min(offset, end)
        results = ",".join(self._page_resource(row) for row in matched[offset:end])
        has_more = end < len(matched)
        next_cursor = json.dumps(f"rows-{end}") if has_more else "null"
        return 200, (
            f'{{"object":"list","results":[{results}],"next_cursor":{next_cursor},'
            f'"has_more":{"true" if has_more else "false"},"type":"page_or_data_source"}}'
        )

    def _page_resource(self, row: _StoredRow) -> str:
        return json.dumps(
            {
                "object": "page",
                "id": row.id,
                "created_time": _notion_time(row.created),
                "last_edited_time": _notion_time(row.last_edited),
                "in_trash": row.is_in_trash,
                "is_archived": False,
                "url": "https://www.notion.so/" + row.id.replace("-", ""),
                "parent": {
                    "type": "data_source_id",
                    "data_source_id": self.data_source_id,
                    "database_id": self.database_id,
                },
                "properties": {
                    "Name": {
                        "id": "title",
                        "type": "title",
                        "title": [{"type": "text", "text": {"content": row.title}, "plain_text": row.title}],
                    }
                },
            }
        )


def _notion_time(value: datetime) -> str:
    return value.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%S.") + f"{value.microsecond // 1000:03d}Z"


def _notion_error(status: int, code: str) -> str:
    return json.dumps({"object": "error", "status": status, "code": code, "request_id": "fake-request"})
