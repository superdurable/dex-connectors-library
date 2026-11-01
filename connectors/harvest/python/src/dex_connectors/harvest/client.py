# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Harvest provider reads and Go-compatible branch classification."""

import json
import re
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any, TypeVar
from urllib.parse import parse_qs, urlencode, urlsplit

import httpx
from dex_connectors.sdk import (
    BranchDefinition,
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    OperationRef,
    QueryAttempt,
    QueryDefinition,
    ReauthorizationRequiredError,
    Receipt,
    SecretString,
    StepDefaults,
)
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)
from dex_connectors.sdk.retry_policy import default_connector_step_execute_retry_policy

from .models import (
    ListProjectsInput,
    ListTimeEntriesInput,
    Project,
    ProjectPage,
    TimeEntry,
    TimeEntryPage,
    TimeEntryProject,
)

CONNECTOR_ID = "harvest"
LIST_PROJECTS_OPERATION_ID = "listProjects"
LIST_TIME_ENTRIES_OPERATION_ID = "listTimeEntries"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"
INVALID_RESPONSE = "invalidResponse"
DEFECT = "defect"
_DEFAULT_BASE_URL = "https://api.harvestapp.com/v2"
_DEFAULT_TIMEOUT_SECONDS = 30
_DEFAULT_MAX_RESPONSE_BYTES = 4_194_304
_DECIMAL_ID = re.compile(r"[1-9][0-9]{0,18}")
_SAFE_KEY = re.compile(r"[A-Za-z0-9_-]{1,128}")
_STEP_DEFAULTS = StepDefaults(
    timedelta(seconds=45), 2, execute_retry=default_connector_step_execute_retry_policy()
)
_LIST_BRANCHES = (
    BranchDefinition(LISTED, "Harvest returned the requested data."),
    BranchDefinition(PROVIDER_REJECTED, "Harvest conclusively rejected the read.", True),
    BranchDefinition(INVALID_RESPONSE, "Harvest returned malformed or oversized JSON.", True),
    BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
)
LIST_PROJECTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_PROJECTS_OPERATION_ID), _LIST_BRANCHES, _STEP_DEFAULTS
)
LIST_TIME_ENTRIES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_TIME_ENTRIES_OPERATION_ID), _LIST_BRANCHES, _STEP_DEFAULTS
)

Output = TypeVar("Output")


@dataclass(frozen=True, slots=True)
class Config:
    """Harvest settings; account_id is required, other zero or blank values use manifest defaults."""

    account_id: str = ""
    base_url: str = _DEFAULT_BASE_URL
    timeout_seconds: float = _DEFAULT_TIMEOUT_SECONDS
    max_response_bytes: int = _DEFAULT_MAX_RESPONSE_BYTES


@dataclass(frozen=True, slots=True)
class Credentials:
    """Harvest personal access token, resolved anew for every provider attempt."""

    api_key: SecretString


class Client:
    """One redirect-safe HTTP client and call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None:
            raise ValueError("Harvest configuration is required")
        if credentials is None:
            raise ValueError("Harvest credential source is required")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("Harvest timeout and max response bytes must be positive")
        if not isinstance(config.account_id, str) or _DECIMAL_ID.fullmatch(config.account_id) is None:
            raise ValueError("Harvest accountId must be a decimal ID")
        self.account_id = config.account_id
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def list_projects(self) -> "ListProjectsOperation":
        """Bind the project page Query to this client."""
        return ListProjectsOperation(self)

    def list_time_entries(self) -> "ListTimeEntriesOperation":
        """Bind the time entry page Query to this client."""
        return ListTimeEntriesOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def invoke_get(
        self,
        call: Call,
        operation: str,
        path: str,
        empty: Output,
        success: str,
        not_found: str,
        decode: Callable[[Any], Output],
    ) -> QueryAttempt[Output]:
        """Send one GET and classify it exactly like the Go invokeGet helper."""
        try:
            credentials = await self.credentials.resolve(call)
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            return _failed(
                operation, DEFECT, empty, FailureKind.AUTHENTICATION, "connection credentials are unavailable"
            )
        key = credentials.api_key.reveal()
        if not key or not is_header_safe_credential(key):
            return _failed(
                operation, DEFECT, empty, FailureKind.AUTHENTICATION, "connection credentials are invalid"
            )
        headers = {
            "Accept": "application/json",
            "Authorization": "Bearer " + key,
            "Harvest-Account-Id": self.account_id,
            "User-Agent": "superdurable-harvest-connector",
        }
        try:
            request = self.http_client.build_request("GET", self.base_url + path, headers=headers)
        except (httpx.HTTPError, ValueError):
            return _failed(operation, DEFECT, empty, FailureKind.LOCAL_DEFECT, "request cannot be built")
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _retry(operation, FailureKind.AVAILABILITY, "provider request failed", 0)
        try:
            status = response.status_code
            if status != 200:
                retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
                if status == 429:
                    return _retry(operation, FailureKind.RATE_LIMIT, "provider rate limit", retry_after)
                if status in (408, 409) or status >= 500:
                    return _retry(
                        operation, FailureKind.AVAILABILITY, "provider temporarily unavailable", retry_after
                    )
                if status == 404 and not_found != PROVIDER_REJECTED:
                    return _failed(
                        operation, not_found, empty, FailureKind.NOT_FOUND, "provider resource was not found"
                    )
                if status == 401:
                    return _failed(
                        operation,
                        PROVIDER_REJECTED,
                        empty,
                        FailureKind.AUTHENTICATION,
                        "provider rejected the API key",
                    )
                if status == 403:
                    return _failed(
                        operation,
                        PROVIDER_REJECTED,
                        empty,
                        FailureKind.AUTHORIZATION,
                        "provider denied the request",
                    )
                return _failed(
                    operation,
                    PROVIDER_REJECTED,
                    empty,
                    FailureKind.PROVIDER_REJECTION,
                    f"provider rejected HTTP {status}",
                )
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _failed(
                    operation,
                    INVALID_RESPONSE,
                    empty,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "provider response exceeded configured limit",
                )
            except ProviderTransportError:
                return _retry(operation, FailureKind.AVAILABILITY, "provider response was interrupted", 0)
            try:
                value = decode(json.loads(body))
            except (ValueError, TypeError, UnicodeDecodeError):
                return _failed(
                    operation,
                    INVALID_RESPONSE,
                    empty,
                    FailureKind.PROTOCOL,
                    "provider returned unusable JSON",
                )
            return QueryAttempt.branch_result(success, value, Receipt())
        finally:
            await response.aclose()


class ListProjectsOperation:
    """Read one cursor page of Harvest projects."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Harvest client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return LIST_PROJECTS_DEFINITION

    async def invoke(self, call: Call, input: ListProjectsInput) -> QueryAttempt[ProjectPage]:
        """Validate the page, read once, then select a branch or safe retry."""
        operation = LIST_PROJECTS_OPERATION_ID
        if not isinstance(input, ListProjectsInput):
            return _failed(operation, DEFECT, ProjectPage(), FailureKind.VALIDATION, "page input is invalid")
        problem = _page_problem(input.cursor, input.per_page)
        if problem:
            return _failed(operation, DEFECT, ProjectPage(), FailureKind.VALIDATION, problem)
        return await self.client.invoke_get(
            call,
            operation,
            _page_path("/projects", input.cursor, input.per_page),
            ProjectPage(),
            LISTED,
            PROVIDER_REJECTED,
            _decode_project_page,
        )


class ListTimeEntriesOperation:
    """Read one cursor page of Harvest time entries."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Harvest client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return LIST_TIME_ENTRIES_DEFINITION

    async def invoke(self, call: Call, input: ListTimeEntriesInput) -> QueryAttempt[TimeEntryPage]:
        """Validate the page, read once, then select a branch or safe retry."""
        operation = LIST_TIME_ENTRIES_OPERATION_ID
        if not isinstance(input, ListTimeEntriesInput):
            return _failed(operation, DEFECT, TimeEntryPage(), FailureKind.VALIDATION, "page input is invalid")
        problem = _page_problem(input.cursor, input.per_page)
        if problem:
            return _failed(operation, DEFECT, TimeEntryPage(), FailureKind.VALIDATION, problem)
        return await self.client.invoke_get(
            call,
            operation,
            _page_path("/time_entries", input.cursor, input.per_page),
            TimeEntryPage(),
            LISTED,
            PROVIDER_REJECTED,
            _decode_time_entry_page,
        )


def _failed(
    operation: str, branch: str, value: Output, kind: FailureKind, message: str
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(
        branch, value, Receipt(), Failure(kind, CONNECTOR_ID, operation, message)
    )


def _retry(operation: str, kind: FailureKind, message: str, after_seconds: float) -> QueryAttempt[Any]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, operation, message), after_seconds)


def _page_problem(cursor: object, per_page: object) -> str:
    if type(per_page) is not int or not 0 <= per_page <= 2000:
        return "perPage must be 1 to 2000 or zero"
    if not isinstance(cursor, str) or (cursor and _SAFE_KEY.fullmatch(cursor) is None):
        return "cursor is invalid"
    return ""


def _page_path(path: str, cursor: str, per_page: int) -> str:
    parameters = {"per_page": str(per_page or 100)}
    if cursor:
        parameters["cursor"] = cursor
    return path + "?" + urlencode(sorted(parameters.items()))


def _next_cursor(document: dict[str, Any], path: str) -> str:
    links = document.get("links")
    if links is None:
        return ""
    if not isinstance(links, dict):
        raise ValueError
    next_url = links.get("next")
    if next_url is None or next_url == "":
        return ""
    if not isinstance(next_url, str):
        raise ValueError
    parsed = urlsplit(next_url)
    if (
        parsed.scheme != "https"
        or parsed.netloc != "api.harvestapp.com"
        or parsed.path != "/v2" + path
        or parsed.fragment
    ):
        raise ValueError
    cursor = parse_qs(parsed.query).get("cursor", [""])[0]
    if _SAFE_KEY.fullmatch(cursor) is None:
        raise ValueError
    return cursor


def _decode_project_page(document: Any) -> ProjectPage:
    if not isinstance(document, dict) or not isinstance(document.get("projects"), list):
        raise ValueError
    projects = tuple(
        Project(_integer(project, "id"), _string(project, "name"), _boolean(project, "is_active"))
        for project in map(_object, document["projects"])
    )
    return ProjectPage(projects, _next_cursor(document, "/projects"))


def _decode_time_entry_page(document: Any) -> TimeEntryPage:
    if not isinstance(document, dict) or not isinstance(document.get("time_entries"), list):
        raise ValueError
    entries = tuple(
        TimeEntry(
            _integer(entry, "id"),
            TimeEntryProject(_integer(_object(entry.get("project") or {}), "id")),
            _number(entry, "hours"),
            _string(entry, "spent_date"),
        )
        for entry in map(_object, document["time_entries"])
    )
    return TimeEntryPage(entries, _next_cursor(document, "/time_entries"))


def _object(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError
    return value


def _string(document: dict[str, Any], name: str) -> str:
    value = document.get(name)
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError
    return value


def _integer(document: dict[str, Any], name: str) -> int:
    value = document.get(name)
    if value is None:
        return 0
    if type(value) is not int:
        raise ValueError
    return value


def _number(document: dict[str, Any], name: str) -> float:
    value = document.get(name)
    if value is None:
        return 0.0
    if type(value) not in (int, float):
        raise ValueError
    return float(value)


def _boolean(document: dict[str, Any], name: str) -> bool:
    value = document.get(name)
    if value is None:
        return False
    if not isinstance(value, bool):
        raise ValueError
    return value
