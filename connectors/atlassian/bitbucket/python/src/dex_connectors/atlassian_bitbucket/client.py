# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bitbucket provider reads and Go-compatible branch classification."""

import json
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any, TypeVar
from urllib.parse import quote, urlencode

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
    ListPullRequestsInput,
    ListRepositoriesInput,
    PullRequest,
    PullRequestPage,
    Repository,
    RepositoryPage,
)

CONNECTOR_ID = "bitbucket"
LIST_REPOSITORIES_OPERATION_ID = "listRepositories"
LIST_PULL_REQUESTS_OPERATION_ID = "listPullRequests"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
INVALID_RESPONSE = "invalidResponse"
DEFECT = "defect"
_DEFAULT_BASE_URL = "https://api.bitbucket.org/2.0"
_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 2_097_152
_STEP_DEFAULTS = StepDefaults(
    timedelta(seconds=30), 2, execute_retry=default_connector_step_execute_retry_policy()
)
_SHARED_BRANCHES = (
    BranchDefinition(NOT_FOUND, "The requested resource was not found.", True),
    BranchDefinition(PROVIDER_REJECTED, "The provider conclusively rejected the request.", True),
    BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
    BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
)
LIST_REPOSITORIES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_REPOSITORIES_OPERATION_ID),
    (BranchDefinition(LISTED, "List repositories owned by a Bitbucket workspace."), *_SHARED_BRANCHES),
    _STEP_DEFAULTS,
)
LIST_PULL_REQUESTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_PULL_REQUESTS_OPERATION_ID),
    (BranchDefinition(LISTED, "List pull requests for a Bitbucket repository."), *_SHARED_BRANCHES),
    _STEP_DEFAULTS,
)

Output = TypeVar("Output")


@dataclass(frozen=True, slots=True)
class Config:
    """Bitbucket HTTP settings; zero or blank values use manifest defaults."""

    base_url: str = _DEFAULT_BASE_URL
    timeout_seconds: float = _DEFAULT_TIMEOUT_SECONDS
    max_response_bytes: int = _DEFAULT_MAX_RESPONSE_BYTES


@dataclass(frozen=True, slots=True)
class Credentials:
    """Bitbucket access token, resolved anew for every provider attempt."""

    access_token: SecretString


class Client:
    """One redirect-safe HTTP client and call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None:
            raise ValueError("Bitbucket configuration is required")
        if credentials is None:
            raise ValueError("Bitbucket credential source is required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("timeout and maxResponseBytes must be positive")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def list_repositories(self) -> "ListRepositoriesOperation":
        """Bind the repository page Query to this client."""
        return ListRepositoriesOperation(self)

    def list_pull_requests(self) -> "ListPullRequestsOperation":
        """Bind the pull request page Query to this client."""
        return ListPullRequestsOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def authorize(self, call: Call) -> dict[str, str] | None:
        """Return the authentication headers, or None when credentials are unusable."""
        try:
            credentials = await self.credentials.resolve(call)
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            return None
        token = credentials.access_token.reveal()
        if not token or not is_header_safe_credential(token):
            return None
        return {"Authorization": "Bearer " + token}

    async def execute_read(
        self,
        call: Call,
        operation: str,
        path: str,
        query: dict[str, str],
        empty: Output,
        success: str,
        decode: Callable[[Any], Output],
    ) -> QueryAttempt[Output]:
        """Send one GET and classify it exactly like the Go executeRead helper."""
        endpoint = self.base_url + path
        if query:
            endpoint += "?" + urlencode(sorted(query.items()))
        headers = await self.authorize(call)
        if headers is None:
            return _failed(
                operation, DEFECT, empty, FailureKind.AUTHENTICATION, "Bitbucket credentials are unavailable"
            )
        try:
            request = self.http_client.build_request(
                "GET", endpoint, headers={"Accept": "application/json", **headers}
            )
        except (httpx.HTTPError, ValueError):
            return _failed(
                operation, DEFECT, empty, FailureKind.LOCAL_DEFECT, "provider request could not be built"
            )
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _retry(operation, FailureKind.AVAILABILITY, "provider read failed", 0)
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(
                    operation, FailureKind.RATE_LIMIT, "provider rate limited the read", retry_after
                )
            if status == 408 or status >= 500:
                return _retry(
                    operation,
                    FailureKind.AVAILABILITY,
                    "provider read is temporarily unavailable",
                    retry_after,
                )
            if status == 404:
                return _failed(
                    operation, NOT_FOUND, empty, FailureKind.PROVIDER_REJECTION, "resource was not found"
                )
            if status == 401:
                return _failed(
                    operation,
                    PROVIDER_REJECTED,
                    empty,
                    FailureKind.AUTHENTICATION,
                    "provider rejected credentials",
                )
            if status == 403:
                return _failed(
                    operation, PROVIDER_REJECTED, empty, FailureKind.AUTHORIZATION, "provider denied access"
                )
            if status != 200:
                return _failed(
                    operation,
                    PROVIDER_REJECTED,
                    empty,
                    FailureKind.PROVIDER_REJECTION,
                    f"provider rejected read with HTTP {status}",
                )
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _failed(
                    operation,
                    INVALID_RESPONSE,
                    empty,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "provider response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(operation, FailureKind.AVAILABILITY, "provider response was interrupted", 0)
            try:
                value = decode(json.loads(body))
            except (ValueError, TypeError, UnicodeDecodeError):
                return _failed(
                    operation, INVALID_RESPONSE, empty, FailureKind.PROTOCOL, "provider returned invalid JSON"
                )
            return QueryAttempt.branch_result(success, value, Receipt())
        finally:
            await response.aclose()


class ListRepositoriesOperation:
    """Read the first page of a Bitbucket workspace's repositories."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Bitbucket client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return LIST_REPOSITORIES_DEFINITION

    async def invoke(self, call: Call, input: ListRepositoriesInput) -> QueryAttempt[RepositoryPage]:
        """Validate the workspace, read once, then select a branch or safe retry."""
        operation = LIST_REPOSITORIES_OPERATION_ID
        if not isinstance(input, ListRepositoriesInput) or not is_valid_path_segment(input.workspace):
            return _failed(
                operation,
                DEFECT,
                RepositoryPage(),
                FailureKind.VALIDATION,
                "workspace must be a path segment",
            )
        path = "/repositories/" + quote(input.workspace, safe="")
        return await self.client.execute_read(
            call, operation, path, {}, RepositoryPage(), LISTED, _decode_repository_page
        )


class ListPullRequestsOperation:
    """Read the first page of a Bitbucket repository's pull requests."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Bitbucket client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return LIST_PULL_REQUESTS_DEFINITION

    async def invoke(self, call: Call, input: ListPullRequestsInput) -> QueryAttempt[PullRequestPage]:
        """Validate the workspace and repository, read once, then select a branch or safe retry."""
        operation = LIST_PULL_REQUESTS_OPERATION_ID
        if not isinstance(input, ListPullRequestsInput) or not is_valid_path_segment(input.workspace):
            return _failed(
                operation,
                DEFECT,
                PullRequestPage(),
                FailureKind.VALIDATION,
                "workspace must be a path segment",
            )
        if not is_valid_path_segment(input.repository):
            return _failed(
                operation,
                DEFECT,
                PullRequestPage(),
                FailureKind.VALIDATION,
                "repository must be a path segment",
            )
        path = (
            "/repositories/"
            + quote(input.workspace, safe="")
            + "/"
            + quote(input.repository, safe="")
            + "/pullrequests"
        )
        return await self.client.execute_read(
            call, operation, path, {}, PullRequestPage(), LISTED, _decode_pull_request_page
        )


def is_valid_path_segment(value: object) -> bool:
    """Accept one non-dot path segment without separators or control characters."""
    return (
        isinstance(value, str)
        and value not in ("", ".", "..")
        and not any(character in "/?#" for character in value)
        and all(ord(character) >= 32 for character in value)
    )


def _failed(
    operation: str, branch: str, value: Output, kind: FailureKind, message: str
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(
        branch, value, Receipt(), Failure(kind, CONNECTOR_ID, operation, message)
    )


def _retry(operation: str, kind: FailureKind, message: str, after_seconds: float) -> QueryAttempt[Any]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, operation, message), after_seconds)


def _decode_repository_page(document: Any) -> RepositoryPage:
    document = _object(document)
    return RepositoryPage(
        tuple(
            Repository(
                _string(item, "uuid"),
                _string(item, "name"),
                _string(item, "full_name"),
                _boolean(item, "is_private"),
            )
            for item in map(_object, _values(document))
        ),
        _string(document, "next"),
        _integer(document, "page"),
        _integer(document, "pagelen"),
    )


def _decode_pull_request_page(document: Any) -> PullRequestPage:
    document = _object(document)
    return PullRequestPage(
        tuple(
            PullRequest(_integer(item, "id"), _string(item, "title"), _string(item, "state"))
            for item in map(_object, _values(document))
        ),
        _string(document, "next"),
        _integer(document, "page"),
        _integer(document, "pagelen"),
    )


def _values(document: dict[str, Any]) -> list[Any]:
    values = document.get("values")
    if not isinstance(values, list):
        raise ValueError
    return values


def _integer(document: dict[str, Any], name: str) -> int:
    value = document.get(name)
    if value is None:
        return 0
    if type(value) is not int:
        raise ValueError
    return value


def _boolean(document: dict[str, Any], name: str) -> bool:
    value = document.get(name)
    if value is None:
        return False
    if not isinstance(value, bool):
        raise ValueError
    return value


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
