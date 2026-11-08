# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Microsoft Graph To Do collection queries with Go-compatible outcomes."""

from __future__ import annotations

import json
from datetime import UTC, datetime
from typing import Any, TypeVar
from urllib.parse import quote, urlsplit

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    QueryAttempt,
    QueryDefinition,
    ReauthorizationRequiredError,
    Receipt,
)
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import LIST_TASK_LISTS_DEFINITION, LIST_TASKS_DEFINITION
from .models import ListTaskListsInput, ListTasksInput, Task, TaskList, TaskListsPage, TasksPage

_GRAPH_ROOT = "https://graph.microsoft.com/v1.0"
_MAX_PAGES = 20
_MAX_ITEMS = 2000
_Output = TypeVar("_Output")


class _ReadFailure(Exception):
    def __init__(self, branch: str, kind: FailureKind, message: str, retry_after: float = 0) -> None:
        super().__init__(message)
        self.branch = branch
        self.kind = kind
        self.message = message
        self.retry_after = retry_after


def _attempt(operation: str, result: _ReadFailure, empty: _Output) -> QueryAttempt[_Output]:
    failure = Failure(result.kind, "microsoft-todo", operation, result.message)
    if not result.branch:
        return QueryAttempt.retry(failure, result.retry_after)
    return QueryAttempt.branch_result(result.branch, empty, Receipt(), failure)


def _decode_records(body: bytes) -> tuple[list[dict[str, Any]], str]:
    raw: Any = json.loads(body)
    if not isinstance(raw, dict) or not isinstance(raw.get("value"), list):
        raise ValueError("invalid collection")
    records = raw["value"]
    if not all(isinstance(record, dict) for record in records):
        raise ValueError("invalid collection item")
    next_link = raw.get("@odata.nextLink", "")
    if not isinstance(next_link, str):
        raise ValueError("invalid next page")
    return records, next_link


class Client:
    """Read the signed-in person's To Do collections with renewable credentials."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Microsoft To Do configuration and credentials are required")
        if type(config.max_response_bytes) is not int or config.max_response_bytes <= 0:
            raise ValueError("Microsoft To Do max response bytes must be positive")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(15)
        self._owns_client = http_client is None

    def list_task_lists(self) -> ListTaskListsOperation:
        """Bind the listTaskLists Query."""
        return ListTaskListsOperation(self)

    def list_tasks(self) -> ListTasksOperation:
        """Bind the listTasks Query."""
        return ListTasksOperation(self)

    async def aclose(self) -> None:
        """Close only the HTTP client this connector constructed."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _read_collection(self, call: Call, path: str) -> list[dict[str, Any]]:
        try:
            credential = await self.credentials.resolve(call)
            token = credential.access_token.reveal()
        except ReauthorizationRequiredError as error:
            raise _ReadFailure(
                "defect", FailureKind.AUTHENTICATION, "To Do connection needs authorization"
            ) from error
        except Exception as error:
            raise _ReadFailure("", FailureKind.TRANSPORT, "To Do credential refresh failed") from error
        if not is_header_safe_credential(token):
            raise _ReadFailure("defect", FailureKind.AUTHENTICATION, "To Do access token is unavailable")
        target = _GRAPH_ROOT + path
        records: list[dict[str, Any]] = []
        seen: set[str] = set()
        for _page in range(_MAX_PAGES):
            if target in seen:
                raise _ReadFailure("invalidResponse", FailureKind.PROTOCOL, "Graph repeated a page")
            seen.add(target)
            try:
                request = self.http_client.build_request(
                    "GET", target, headers={"Authorization": "Bearer " + token, "Accept": "application/json"}
                )
                response, _ = await send_traced(self.http_client, request)
            except (ProviderTransportError, httpx.HTTPError, ValueError) as error:
                raise _ReadFailure("", FailureKind.TRANSPORT, "Graph request failed") from error
            try:
                is_too_large = False
                is_interrupted = False
                try:
                    body = await read_bounded_body(response, self.max_response_bytes)
                except BodyTooLargeError:
                    body = b""
                    is_too_large = True
                except ProviderTransportError:
                    body = b""
                    is_interrupted = True
                status = response.status_code
                if status == 429 or status == 408 or status >= 500:
                    delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
                    raise _ReadFailure(
                        "", FailureKind.AVAILABILITY, "Graph is temporarily unavailable", delay
                    )
                if status == 404:
                    raise _ReadFailure("notFound", FailureKind.NOT_FOUND, "To Do list was not found")
                if status != 200:
                    raise _ReadFailure(
                        "providerRejected", FailureKind.PROVIDER_REJECTION, "Graph rejected the request"
                    )
                if is_too_large:
                    raise _ReadFailure(
                        "invalidResponse", FailureKind.RESPONSE_TOO_LARGE, "Graph response exceeded the limit"
                    )
                if is_interrupted:
                    raise _ReadFailure("", FailureKind.TRANSPORT, "Graph response was interrupted")
            finally:
                await response.aclose()
            try:
                page_records, next_link = _decode_records(body)
            except (ValueError, TypeError, UnicodeDecodeError) as error:
                raise _ReadFailure(
                    "invalidResponse", FailureKind.PROTOCOL, "Graph returned invalid collection JSON"
                ) from error
            records.extend(page_records)
            if len(records) > _MAX_ITEMS:
                raise _ReadFailure(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Graph collection exceeded the item limit",
                )
            if not next_link:
                return records
            parsed = urlsplit(next_link)
            if (
                parsed.scheme != "https"
                or parsed.netloc != "graph.microsoft.com"
                or parsed.path != "/v1.0" + path
                or parsed.fragment
            ):
                raise _ReadFailure(
                    "invalidResponse", FailureKind.PROTOCOL, "Graph returned an unsafe next page URL"
                )
            target = next_link
        raise _ReadFailure(
            "invalidResponse", FailureKind.RESPONSE_TOO_LARGE, "Graph collection exceeded the page limit"
        )


class ListTaskListsOperation:
    """Read the signed-in person's bounded task-list collection."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Microsoft To Do client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query contract."""
        return LIST_TASK_LISTS_DEFINITION

    async def invoke(self, call: Call, input: ListTaskListsInput) -> QueryAttempt[TaskListsPage]:
        """Classify Graph list and pagination outcomes consistently with Go."""
        try:
            records = await self.client._read_collection(call, "/me/todo/lists")
            lists = tuple(TaskList(record["id"], record["displayName"]) for record in records)
            if any(
                not isinstance(item.id, str)
                or not item.id
                or not isinstance(item.display_name, str)
                or not item.display_name
                for item in lists
            ):
                raise ValueError("invalid list")
        except _ReadFailure as result:
            if result.branch == "notFound":
                result.branch = "providerRejected"
            return _attempt("listTaskLists", result, TaskListsPage())
        except (KeyError, TypeError, ValueError):
            return _attempt(
                "listTaskLists",
                _ReadFailure("invalidResponse", FailureKind.PROTOCOL, "Graph returned an invalid task list"),
                TaskListsPage(),
            )
        return QueryAttempt.branch_result("listed", TaskListsPage(lists), Receipt())


class ListTasksOperation:
    """Read the bounded tasks in one selected To Do list."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Microsoft To Do client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query contract."""
        return LIST_TASKS_DEFINITION

    async def invoke(self, call: Call, input: ListTasksInput) -> QueryAttempt[TasksPage]:
        """Classify Graph task reads consistently with Go."""
        if (
            not isinstance(input, ListTasksInput)
            or not isinstance(input.list_id, str)
            or not input.list_id.strip()
            or "\r" in input.list_id
            or "\n" in input.list_id
        ):
            return _attempt(
                "listTasks",
                _ReadFailure("defect", FailureKind.VALIDATION, "To Do list ID is required"),
                TasksPage(),
            )
        try:
            records = await self.client._read_collection(
                call, "/me/todo/lists/" + quote(input.list_id, safe="") + "/tasks"
            )
            tasks = tuple(
                Task(record["id"], record["title"], "" if record.get("status") is None else record["status"])
                for record in records
            )
            if any(
                not isinstance(item.id, str)
                or not item.id
                or not isinstance(item.title, str)
                or not item.title
                or not isinstance(item.status, str)
                for item in tasks
            ):
                raise ValueError("invalid task")
        except _ReadFailure as result:
            return _attempt("listTasks", result, TasksPage())
        except (KeyError, TypeError, ValueError):
            return _attempt(
                "listTasks",
                _ReadFailure("invalidResponse", FailureKind.PROTOCOL, "Graph returned an invalid task"),
                TasksPage(),
            )
        return QueryAttempt.branch_result("listed", TasksPage(tasks), Receipt())
