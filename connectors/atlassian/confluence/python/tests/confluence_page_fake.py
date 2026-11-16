# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""An in-process Confluence site for pageChanged tests, ported from the Go confluencepagefake package.

It serves the accessible-resources lookup, CQL content search with the subset of CQL the Trigger sends,
and page reads in storage format. It checks a bearer token but never echoes it.
"""

import html
import json
import re
import threading
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any, Final

import httpx

ACCESS_TOKEN: Final = "confluence-fake-" + "access-token"
CLOUD_ID: Final = "11223344-a1b2-4b33-8c44-def123456789"
_SITE_BASE: Final = "https://ops.atlassian.net/wiki"
_SPACE_CLAUSE: Final = re.compile(r'space = "([^"]+)"')
_LAST_MODIFIED_CLAUSE: Final = re.compile(r'lastmodified >= "(\d{4}-\d{2}-\d{2} \d{2}:\d{2})"')
_CURSOR: Final = re.compile(r"g(\d+)-o(\d+)-q(\d+)\Z")


@dataclass
class _Page:
    id: str
    space_key: str
    title: str
    body: str
    version: int
    last_modified_at: datetime
    created_at: datetime


def _format(moment: datetime) -> str:
    return moment.astimezone(UTC).isoformat(timespec="microseconds").replace("+00:00", "Z")


class ConfluencePageSite:
    """One fake Confluence site; every method is safe to call from the test and the transport."""

    def __init__(self, cloud_id: str = CLOUD_ID, clock: Callable[[], datetime] | None = None) -> None:
        self.cloud_id = cloud_id
        self._clock = clock or (lambda: datetime.now(UTC))
        self._lock = threading.Lock()
        self._pages: dict[str, _Page] = {}
        self._next_page_id = 600000
        self._cursor_generation = 0
        self._throttled_searches = 0
        self._retry_after_seconds = 0
        self.search_statuses: list[int] = []
        self.cql_queries: list[str] = []

    def transport(self) -> httpx.MockTransport:
        """An httpx transport that answers every request from this site."""
        return httpx.MockTransport(self._handle)

    def publish(self, space_key: str, title: str, body: str) -> str:
        """Create a page at version 1 and return its ID."""
        with self._lock:
            self._next_page_id += 1
            now = self._clock()
            page = _Page(str(self._next_page_id), space_key, title, body, 1, now, now)
            self._pages[page.id] = page
            return page.id

    def edit(self, page_id: str, title: str, body: str) -> None:
        """Save the next version of a page with a new title and body."""
        with self._lock:
            page = self._pages[page_id]
            page.title, page.body, page.version, page.last_modified_at = (
                title,
                body,
                page.version + 1,
                self._clock(),
            )

    def throttle_searches(self, count: int, retry_after_seconds: int) -> None:
        """Make the next count searches answer HTTP 429 with Retry-After in seconds."""
        with self._lock:
            self._throttled_searches, self._retry_after_seconds = count, retry_after_seconds

    def expire_search_cursors(self) -> None:
        """Make every search cursor issued so far answer HTTP 400."""
        with self._lock:
            self._cursor_generation += 1

    def search_requests(self) -> int:
        """How many content searches the site answered, including throttled ones."""
        with self._lock:
            return len(self.search_statuses)

    def _handle(self, request: httpx.Request) -> httpx.Response:
        if request.headers.get("Authorization") != "Bearer " + ACCESS_TOKEN:
            return httpx.Response(401, json={"message": "Unauthorized"})
        prefix = f"/ex/confluence/{self.cloud_id}/wiki"
        path = request.url.path
        if request.method == "GET" and path == "/oauth/token/accessible-resources":
            scopes = ["search:confluence", "read:page:confluence"]
            return httpx.Response(200, json=[{"id": self.cloud_id, "name": "Ops", "scopes": scopes}])
        if request.method == "GET" and path == prefix + "/rest/api/content/search":
            return self._search(request)
        if request.method == "GET" and path.startswith(prefix + "/api/v2/pages/"):
            return self._read_page(path.removeprefix(prefix + "/api/v2/pages/"))
        return httpx.Response(404, json={"message": "Not Found"})

    def _search(self, request: httpx.Request) -> httpx.Response:
        with self._lock:
            if self._throttled_searches > 0:
                self._throttled_searches -= 1
                self.search_statuses.append(429)
                headers = {"Retry-After": str(self._retry_after_seconds)}
                return httpx.Response(429, headers=headers, json={"message": "Rate limit exceeded"})
            cql = request.url.params.get("cql", "")
            self.cql_queries.append(cql)
            try:
                matches = self._matching_pages(cql)
            except ValueError as error:
                self.search_statuses.append(400)
                return httpx.Response(400, json={"message": str(error)})
            limit = int(request.url.params.get("limit", "25") or "25")
            offset = 0
            cursor = request.url.params.get("cursor", "")
            if cursor:
                match = _CURSOR.match(cursor)
                if match is None or int(match[1]) != self._cursor_generation or int(match[3]) != len(cql):
                    self.search_statuses.append(400)
                    return httpx.Response(400, json={"message": "Invalid cursor"})
                offset = int(match[2])
            end = min(offset + limit, len(matches))
            results = [self._search_result(page) for page in matches[offset:end]]
            links: dict[str, Any] = {"base": _SITE_BASE}
            if end < len(matches):
                next_cursor = f"g{self._cursor_generation}-o{end}-q{len(cql)}"
                links["next"] = (
                    "/rest/api/content/search?cql=" + cql.replace(" ", "+") + "&cursor=" + next_cursor
                )
            self.search_statuses.append(200)
            return httpx.Response(200, json={"results": results, "_links": links})

    def _matching_pages(self, cql: str) -> list[_Page]:
        """Apply the CQL subset the Trigger sends; the site's time zone is UTC."""
        if not cql.startswith("type = page") or not cql.endswith("ORDER BY lastmodified DESC"):
            raise ValueError(f"unsupported CQL {json.dumps(cql)}")
        space = _SPACE_CLAUSE.search(cql)
        bound = _LAST_MODIFIED_CLAUSE.search(cql)
        lower_bound = datetime.strptime(bound[1], "%Y-%m-%d %H:%M").replace(tzinfo=UTC) if bound else None
        matches = [
            page
            for page in self._pages.values()
            if (space is None or page.space_key == space[1])
            and (lower_bound is None or page.last_modified_at >= lower_bound)
        ]
        matches.sort(key=lambda page: page.id)
        matches.sort(key=lambda page: page.last_modified_at, reverse=True)
        return matches

    def _search_result(self, page: _Page) -> dict[str, Any]:
        return {
            "id": page.id,
            "type": "page",
            "status": "current",
            "title": page.title,
            "space": {"id": 98306, "key": page.space_key},
            "version": {"number": page.version, "when": _format(page.last_modified_at)},
            "_links": {"webui": f"/spaces/{page.space_key}/pages/{page.id}"},
        }

    def _read_page(self, page_id: str) -> httpx.Response:
        with self._lock:
            page = self._pages.get(page_id)
            if page is None:
                return httpx.Response(404, json={"message": "Not Found"})
            return httpx.Response(
                200,
                json={
                    "id": page.id,
                    "status": "current",
                    "title": page.title,
                    "spaceId": "98306",
                    "createdAt": _format(page.created_at),
                    "version": {"number": page.version, "createdAt": _format(page.last_modified_at)},
                    "body": {
                        "storage": {"representation": "storage", "value": f"<p>{html.escape(page.body)}</p>"}
                    },
                    "_links": {"webui": f"/spaces/{page.space_key}/pages/{page.id}", "base": _SITE_BASE},
                },
            )
