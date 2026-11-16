# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""A stateful stand-in for one Jira Cloud site's enhanced JQL search and issue reads.

It mirrors the Go connector's internal/jirasearchfake: it understands exactly the JQL the issueChanged
Trigger sends, encodes page tokens as base64 "<offset>:<jql>", stamps responses with its own clock in a
Date header, and formats times in a non-UTC offset as Jira does. Serve it through `httpx.MockTransport`.
"""

import base64
import json
import re
import threading
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta, timezone
from email.utils import format_datetime
from typing import Any

import httpx

CLOUD_ID = "1324a887-45db-1bf4-1e99-ef0ff456d421"
_TRIGGER_JQL = re.compile(
    r'(?:project = ("?[A-Z0-9_]+"?) AND )?updated >= -([0-9]+)m ORDER BY updated (ASC|DESC)\Z'
)
# Jira formats times in the account's time zone.
_JIRA_ZONE = timezone(timedelta(hours=1))
_UNIX_EPOCH = datetime(1970, 1, 1, tzinfo=UTC)


@dataclass(frozen=True, slots=True)
class Project:
    """One fake project."""

    id: str
    key: str
    name: str


OPERATIONS = Project("10000", "OPS", "Operations")
HUMAN_RESOURCES = Project("10001", "HR", "Human Resources")


@dataclass(frozen=True, slots=True)
class SearchRequest:
    """One recorded search request and the status the fake answered."""

    jql: str
    next_page_token: str
    max_results: int
    status: int


@dataclass(slots=True)
class _StoredIssue:
    id: str
    key: str
    summary: str
    project: Project
    created: datetime
    updated: datetime
    is_unindexed: bool = False


def _truncate_to_millisecond(moment: datetime) -> datetime:
    return moment.replace(microsecond=moment.microsecond // 1000 * 1000)


class JiraSearchSite:
    """A fake site that is safe for use from several threads."""

    def __init__(self, clock: Callable[[], datetime] | None = None) -> None:
        self._lock = threading.Lock()
        self._clock = clock or (lambda: datetime.now(UTC))
        self._issues: dict[str, _StoredIssue] = {}
        self._next_issue_number = 1
        self._rate_limited = 0
        self._rate_limit_status = 429
        self._retry_after = ""
        self._rejects_page_tokens = False
        self._requests: list[SearchRequest] = []

    def transport(self) -> httpx.MockTransport:
        """Return a transport that serves this site."""
        return httpx.MockTransport(self.handle)

    def create(self, project: Project, summary: str) -> str:
        """Add an issue to project and return its key, such as OPS-1."""
        with self._lock:
            number = self._next_issue_number
            self._next_issue_number += 1
            now = _truncate_to_millisecond(self._clock())
            issue = _StoredIssue(str(20000 + number), f"{project.key}-{number}", summary, project, now, now)
            self._issues[issue.key] = issue
            return issue.key

    def update(self, key: str, summary: str) -> None:
        """Change an issue's summary and move its updated time to the fake's clock."""
        with self._lock:
            issue = self._issues[key]
            issue.summary, issue.updated = summary, _truncate_to_millisecond(self._clock())

    def set_indexed(self, key: str, is_indexed: bool) -> None:
        """Hide an issue from search, as Jira's index lag does, or show it again."""
        with self._lock:
            self._issues[key].is_unindexed = not is_indexed

    def issue_id(self, key: str) -> str:
        """Return the numeric ID of key."""
        with self._lock:
            return self._issues[key].id

    def rate_limit_next_requests(self, count: int, retry_after: str) -> None:
        """Answer the next count search requests with 429 and the Retry-After value."""
        with self._lock:
            self._rate_limited, self._rate_limit_status, self._retry_after = count, 429, retry_after

    def overload_next_requests(self, count: int, retry_after: str) -> None:
        """Answer the next count search requests with 503 and the Retry-After value."""
        with self._lock:
            self._rate_limited, self._rate_limit_status, self._retry_after = count, 503, retry_after

    def reject_page_tokens(self, is_rejected: bool) -> None:
        """Answer every search that carries a page token with 400, as for an expired token."""
        with self._lock:
            self._rejects_page_tokens = is_rejected

    def requests(self) -> list[SearchRequest]:
        """Return the recorded search requests in arrival order."""
        with self._lock:
            return list(self._requests)

    def handle(self, request: httpx.Request) -> httpx.Response:
        """Serve one request."""
        prefix = f"/ex/jira/{CLOUD_ID}/rest/api/3"
        if not request.headers.get("Authorization") or not request.url.path.startswith(prefix):
            return httpx.Response(401, json={"errorMessages": ["unauthorized"]})
        path = request.url.path.removeprefix(prefix)
        if request.method == "POST" and path == "/search/jql":
            return self._serve_search(request)
        if request.method == "GET" and path.startswith("/issue/"):
            return self._serve_issue(path.removeprefix("/issue/"))
        return httpx.Response(404, json={"errorMessages": ["not found"]})

    def _serve_search(self, request: httpx.Request) -> httpx.Response:
        try:
            body = json.loads(request.content)
            jql, max_results = str(body.get("jql", "")), int(body.get("maxResults", 0))
            page_token = str(body.get("nextPageToken", ""))
        except (ValueError, TypeError, AttributeError):
            return httpx.Response(400, json={"errorMessages": ["invalid body"]})
        with self._lock:
            headers = {"Date": format_datetime(self._clock().astimezone(UTC), usegmt=True)}
            status, payload = self._search(jql, max_results, page_token, headers)
            self._requests.append(SearchRequest(jql, page_token, max_results, status))
            return httpx.Response(status, json=payload, headers=headers)

    def _search(
        self, jql: str, max_results: int, page_token: str, headers: dict[str, str]
    ) -> tuple[int, Any]:
        if self._rate_limited > 0:
            self._rate_limited -= 1
            headers["Retry-After"] = self._retry_after
            return self._rate_limit_status, {"errorMessages": ["rate limited"]}
        match = _TRIGGER_JQL.match(jql)
        offset = 0
        if page_token:
            try:
                decoded = base64.urlsafe_b64decode(page_token + "=" * (-len(page_token) % 4)).decode()
                offset_text, separator, query = decoded.partition(":")
                offset = int(offset_text)
            except ValueError:
                separator, query = "", ""
            if self._rejects_page_tokens or not separator or query != jql:
                return 400, {"errorMessages": ["invalid nextPageToken"]}
        if match is None or not 1 <= max_results <= 100:
            return 400, {"errorMessages": ["unsupported JQL"]}
        since = self._clock() - timedelta(minutes=int(match[2]))
        since = since.replace(second=0, microsecond=0)
        project = (match[1] or "").strip('"')
        matched = [
            issue
            for issue in self._issues.values()
            if not issue.is_unindexed
            and issue.updated >= since
            and (not project or project in (issue.project.id, issue.project.key))
        ]
        is_descending = match[3] == "DESC"
        matched.sort(key=lambda issue: issue.id)
        matched.sort(key=lambda issue: issue.updated, reverse=is_descending)
        end = min(offset + max_results, len(matched))
        page: dict[str, Any] = {
            "issues": [issue_resource(issue) for issue in matched[min(offset, end) : end]]
        }
        if end < len(matched):
            page["nextPageToken"] = base64.urlsafe_b64encode(f"{end}:{jql}".encode()).decode().rstrip("=")
        page["isLast"] = end >= len(matched)
        return 200, page

    def _serve_issue(self, id_or_key: str) -> httpx.Response:
        with self._lock:
            for issue in self._issues.values():
                if id_or_key in (issue.key, issue.id):
                    return httpx.Response(200, json=issue_resource(issue))
        return httpx.Response(404, json={"errorMessages": ["Issue does not exist"]})


def _jira_time(moment: datetime) -> str:
    local = moment.astimezone(_JIRA_ZONE)
    return local.strftime("%Y-%m-%dT%H:%M:%S.") + f"{local.microsecond // 1000:03d}" + local.strftime("%z")


def issue_resource(issue: _StoredIssue) -> dict[str, Any]:
    """Return the issue as Jira's search and issue reads return it."""
    return {
        "id": issue.id,
        "key": issue.key,
        "fields": {
            "summary": issue.summary,
            "status": {"id": "1", "name": "To Do", "statusCategory": {"key": "new"}},
            "issuetype": {"id": "10001", "name": "Task"},
            "project": {"id": issue.project.id, "key": issue.project.key, "name": issue.project.name},
            "labels": [],
            "created": _jira_time(issue.created),
            "updated": _jira_time(issue.updated),
        },
    }
