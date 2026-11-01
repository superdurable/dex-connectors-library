# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Sentry provider calls and Go-compatible branch classification."""

import json
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any, TypeVar
from urllib.parse import quote

import httpx
from dex_connectors.sdk import (
    BranchDefinition,
    Call,
    CallID,
    CredentialProvider,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    OperationRef,
    QueryAttempt,
    QueryDefinition,
    ReauthorizationRequiredError,
    Receipt,
    SecretString,
    StepDefaults,
)
from dex_connectors.sdk.providerhttp import (
    MAX_ERROR_BODY_BYTES,
    BodyTooLargeError,
    ProviderTransportError,
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    read_error_tokens,
    send_traced,
    validate_base_url,
)
from dex_connectors.sdk.retry_policy import default_connector_step_execute_retry_policy

from .models import GetIssueInput, Issue, IssuePage, ListIssuesInput, UpdateIssueStatusInput

CONNECTOR_ID = "sentry"
LIST_ISSUES_OPERATION_ID = "listIssues"
GET_ISSUE_OPERATION_ID = "getIssue"
UPDATE_ISSUE_STATUS_OPERATION_ID = "updateIssueStatus"
LISTED = "listed"
FOUND = "found"
UPDATED = "updated"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
INVALID_RESPONSE = "invalidResponse"
DEFECT = "defect"
ISSUE_STATUSES = ("resolved", "unresolved", "ignored")
_DEFAULT_BASE_URL = "https://sentry.io/api/0"
_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 2_097_152
_STEP_DEFAULTS = StepDefaults(
    timedelta(seconds=30), 2, execute_retry=default_connector_step_execute_retry_policy()
)
_NOT_FOUND_BRANCH = BranchDefinition(NOT_FOUND, "The requested resource does not exist.", True)
_SHARED_BRANCHES = (
    BranchDefinition(PROVIDER_REJECTED, "The provider definitively rejected the request.", True),
    BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
    BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
)
LIST_ISSUES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_ISSUES_OPERATION_ID),
    (BranchDefinition(LISTED, "Sentry returned the requested result."), *_SHARED_BRANCHES),
    _STEP_DEFAULTS,
)
GET_ISSUE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_ISSUE_OPERATION_ID),
    (BranchDefinition(FOUND, "Sentry returned the requested result."), _NOT_FOUND_BRANCH, *_SHARED_BRANCHES),
    _STEP_DEFAULTS,
)
UPDATE_ISSUE_STATUS_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_ISSUE_STATUS_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "Sentry returned the requested result."),
        _NOT_FOUND_BRANCH,
        *_SHARED_BRANCHES,
    ),
    _STEP_DEFAULTS,
)

Output = TypeVar("Output")


@dataclass(frozen=True, slots=True)
class Config:
    """Sentry HTTP settings; zero or blank values use manifest defaults."""

    base_url: str = _DEFAULT_BASE_URL
    timeout_seconds: float = _DEFAULT_TIMEOUT_SECONDS
    max_response_bytes: int = _DEFAULT_MAX_RESPONSE_BYTES


@dataclass(frozen=True, slots=True)
class Credentials:
    """Sentry auth token, resolved anew for every provider attempt."""

    api_key: SecretString


@dataclass(frozen=True, slots=True)
class _Outcome:
    """Provider-neutral classification shared by the Query and Mutation helpers."""

    branch: str = ""
    kind: FailureKind | None = None
    message: str = ""
    retry_after_seconds: float = 0
    is_retry: bool = False
    content: bytes = b""


class Client:
    """One redirect-safe HTTP client and call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None:
            raise ValueError("Sentry configuration is required")
        if credentials is None:
            raise ValueError("Sentry credential source is required")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("Sentry timeout and max response bytes must be positive")
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def list_issues(self) -> "ListIssuesOperation":
        """Bind the issue search Query to this client."""
        return ListIssuesOperation(self)

    def get_issue(self) -> "GetIssueOperation":
        """Bind the issue lookup Query to this client."""
        return GetIssueOperation(self)

    def update_issue_status(self) -> "UpdateIssueStatusOperation":
        """Bind the absolute status update Mutation to this client."""
        return UpdateIssueStatusOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def exchange(
        self,
        call: Call,
        method: str,
        path: str,
        body: bytes | None,
        has_not_found_branch: bool,
        is_mutation: bool,
    ) -> _Outcome:
        """Send once and classify exactly like the Go runQuery and runMutation helpers."""
        try:
            credentials = await self.credentials.resolve(call)
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            return _defect_outcome()
        token = credentials.api_key.reveal()
        if not token.strip() or not is_header_safe_credential(token):
            return _defect_outcome()
        headers = {
            "Authorization": "Bearer " + token,
            "Accept": "application/json",
            "User-Agent": "superdurable-sentry/0.1",
        }
        if body is not None:
            headers["Content-Type"] = "application/json"
        try:
            request = self.http_client.build_request(
                method, self.base_url + path, content=body, headers=headers
            )
        except (httpx.HTTPError, ValueError):
            return _defect_outcome()
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _Outcome(kind=FailureKind.AVAILABILITY, message="provider request failed", is_retry=True)
        try:
            return await self._classify(response, has_not_found_branch, is_mutation)
        finally:
            await response.aclose()

    async def _classify(
        self, response: httpx.Response, has_not_found_branch: bool, is_mutation: bool
    ) -> _Outcome:
        status = response.status_code
        retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
        if status == 429:
            return _Outcome(
                kind=FailureKind.RATE_LIMIT,
                message="provider rate limited the request",
                retry_after_seconds=retry_after,
                is_retry=True,
            )
        if status == 408 or status >= 500:
            return _Outcome(
                kind=FailureKind.AVAILABILITY,
                message="provider is temporarily unavailable",
                retry_after_seconds=retry_after,
                is_retry=True,
            )
        if is_mutation and status == 409:
            try:
                conflict = await read_bounded_body(response, MAX_ERROR_BODY_BYTES)
            except (BodyTooLargeError, ProviderTransportError):
                conflict = b""
            if read_error_tokens(conflict, ("/name",)) == ("invalid_idempotent_request",):
                return _Outcome(
                    PROVIDER_REJECTED,
                    FailureKind.PROVIDER_REJECTION,
                    "idempotency key was used for a different request",
                )
            return _Outcome(
                kind=FailureKind.AVAILABILITY,
                message="provider is processing the same request",
                is_retry=True,
            )
        if status == 404 and has_not_found_branch:
            return _Outcome(NOT_FOUND, FailureKind.NOT_FOUND, "provider resource was not found")
        is_success = status in (200, 201, 202) if is_mutation else status == 200
        if not is_success:
            return _Outcome(
                PROVIDER_REJECTED,
                FailureKind.PROVIDER_REJECTION,
                f"provider rejected the request with HTTP {status}",
            )
        try:
            content = await read_bounded_body(response, self.max_response_bytes)
        except BodyTooLargeError:
            return _Outcome(
                INVALID_RESPONSE,
                FailureKind.RESPONSE_TOO_LARGE,
                "provider response exceeded the configured limit",
            )
        except ProviderTransportError:
            return _Outcome(
                kind=FailureKind.AVAILABILITY, message="provider response was interrupted", is_retry=True
            )
        return _Outcome(content=content)


class ListIssuesOperation:
    """List the first page of an organization's issues."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Sentry client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return LIST_ISSUES_DEFINITION

    async def invoke(self, call: Call, input: ListIssuesInput) -> QueryAttempt[IssuePage]:
        """Validate, read once, then select a branch or safe retry."""
        operation = LIST_ISSUES_OPERATION_ID
        if (
            not isinstance(input, ListIssuesInput)
            or not _is_text(input.organization)
            or not isinstance(input.query, str)
        ):
            return _query_failure(
                operation, DEFECT, IssuePage(), FailureKind.VALIDATION, "organization slug is required"
            )
        path = "/organizations/" + _path_segment(input.organization) + "/issues/"
        if input.query:
            path += "?query=" + quote(input.query, safe="")
        outcome = await self.client.exchange(call, "GET", path, None, False, False)
        return _query_attempt(operation, outcome, IssuePage(), LISTED, _decode_issue_page)


class GetIssueOperation:
    """Read one Sentry issue by organization and ID."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Sentry client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return GET_ISSUE_DEFINITION

    async def invoke(self, call: Call, input: GetIssueInput) -> QueryAttempt[Issue]:
        """Validate, read once, then select a branch or safe retry."""
        operation = GET_ISSUE_OPERATION_ID
        if not isinstance(input, GetIssueInput) or not _is_text(input.organization) or not _is_text(input.id):
            return _query_failure(
                operation, DEFECT, Issue(), FailureKind.VALIDATION, "organization and issue ID are required"
            )
        outcome = await self.client.exchange(
            call, "GET", _issue_path(input.organization, input.id), None, True, False
        )
        return _query_attempt(operation, outcome, Issue(), FOUND, _decode_issue)


class UpdateIssueStatusOperation:
    """Set an issue's status; the absolute replacement makes repetition safe."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Sentry client is required")
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return UPDATE_ISSUE_STATUS_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpdateIssueStatusInput) -> IdempotencyKey:
        """Use the stable call ID; Sentry receives no key because the update is absolute."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: UpdateIssueStatusInput) -> MutationAttempt[Issue]:
        """Validate, send one PUT, then select a branch or safe retry."""
        operation = UPDATE_ISSUE_STATUS_OPERATION_ID
        if (
            not isinstance(input, UpdateIssueStatusInput)
            or not _is_text(input.organization)
            or not _is_text(input.id)
        ):
            return _mutation_failure(
                operation, DEFECT, Issue(), FailureKind.VALIDATION, "organization and issue ID are required"
            )
        if input.status not in ISSUE_STATUSES:
            return _mutation_failure(
                operation,
                DEFECT,
                Issue(),
                FailureKind.VALIDATION,
                "status must be resolved, unresolved, or ignored",
            )
        body = json.dumps({"status": input.status}).encode()
        outcome = await self.client.exchange(
            call, "PUT", _issue_path(input.organization, input.id), body, True, True
        )
        if outcome.is_retry:
            return MutationAttempt.retry(_failure(operation, outcome), outcome.retry_after_seconds)
        if outcome.branch:
            return MutationAttempt.branch_result(
                outcome.branch, Issue(), Receipt(), _failure(operation, outcome)
            )
        try:
            issue = _decode_issue(_decode_object(outcome.content))
        except (ValueError, TypeError, UnicodeDecodeError):
            return _mutation_failure(
                operation,
                INVALID_RESPONSE,
                Issue(),
                FailureKind.PROTOCOL,
                "provider response is incomplete or malformed",
            )
        return MutationAttempt.branch_result(UPDATED, issue, Receipt())


def _query_attempt(
    operation: str, outcome: _Outcome, empty: Output, success: str, decode: Callable[[Any], Output]
) -> QueryAttempt[Output]:
    if outcome.is_retry:
        return QueryAttempt.retry(_failure(operation, outcome), outcome.retry_after_seconds)
    if outcome.branch:
        return QueryAttempt.branch_result(outcome.branch, empty, Receipt(), _failure(operation, outcome))
    try:
        value = decode(json.loads(outcome.content))
    except (ValueError, TypeError, UnicodeDecodeError):
        return _query_failure(
            operation,
            INVALID_RESPONSE,
            empty,
            FailureKind.PROTOCOL,
            "provider response is incomplete or malformed",
        )
    return QueryAttempt.branch_result(success, value, Receipt())


def _defect_outcome() -> _Outcome:
    return _Outcome(DEFECT, FailureKind.AUTHENTICATION, "connection credentials or request are invalid")


def _failure(operation: str, outcome: _Outcome) -> Failure:
    return Failure(outcome.kind or FailureKind.LOCAL_DEFECT, CONNECTOR_ID, operation, outcome.message)


def _query_failure(
    operation: str, branch: str, value: Output, kind: FailureKind, message: str
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(
        branch, value, Receipt(), Failure(kind, CONNECTOR_ID, operation, message)
    )


def _mutation_failure(
    operation: str, branch: str, value: Output, kind: FailureKind, message: str
) -> MutationAttempt[Output]:
    return MutationAttempt.branch_result(
        branch, value, Receipt(), Failure(kind, CONNECTOR_ID, operation, message)
    )


def _is_text(value: object) -> bool:
    return isinstance(value, str) and value != ""


def _path_segment(value: str) -> str:
    return quote(value, safe="")


def _issue_path(organization: str, issue_id: str) -> str:
    return "/organizations/" + _path_segment(organization) + "/issues/" + _path_segment(issue_id) + "/"


def _decode_object(content: bytes) -> dict[str, Any]:
    document = json.loads(content)
    if not isinstance(document, dict):
        raise ValueError
    return document


def _decode_issue_page(document: Any) -> IssuePage:
    if not isinstance(document, list):
        raise ValueError
    return IssuePage(tuple(_decode_issue_fields(issue) for issue in document))


def _decode_issue(document: Any) -> Issue:
    issue = _decode_issue_fields(document)
    if not issue.id:
        raise ValueError
    return issue


def _decode_issue_fields(document: Any) -> Issue:
    if not isinstance(document, dict):
        raise ValueError
    return Issue(
        _string(document, "id"),
        _string(document, "shortId"),
        _string(document, "title"),
        _string(document, "status"),
    )


def _string(document: dict[str, Any], name: str) -> str:
    value = document.get(name)
    if value is None:
        for key, candidate in document.items():
            if key.casefold() == name.casefold():
                value = candidate
                break
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError
    return value
