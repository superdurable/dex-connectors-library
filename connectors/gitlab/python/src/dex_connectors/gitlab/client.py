# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""GitLab project, issue, and label operations with Go-compatible branch classification."""

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

from .models import GetProjectInput, Issue, IssuesPage, ListIssuesInput, Project, UpdateIssueLabelsInput

CONNECTOR_ID = "gitlab"
GET_PROJECT_OPERATION_ID = "getProject"
LIST_ISSUES_OPERATION_ID = "listIssues"
UPDATE_ISSUE_LABELS_OPERATION_ID = "updateIssueLabels"
FOUND = "found"
LISTED = "listed"
UPDATED = "updated"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
INVALID_RESPONSE = "invalidResponse"
DEFECT = "defect"
_DEFAULT_BASE_URL = "https://gitlab.com/api/v4"
_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 2_097_152
_PATH_SEGMENT_SAFE = "$&+:=@"
_STEP_DEFAULTS = StepDefaults(
    timedelta(seconds=30), 2, execute_retry=default_connector_step_execute_retry_policy()
)
_STATUS_KINDS = {
    401: FailureKind.AUTHENTICATION,
    403: FailureKind.AUTHORIZATION,
    404: FailureKind.NOT_FOUND,
    400: FailureKind.VALIDATION,
    422: FailureKind.VALIDATION,
}
_SHARED_BRANCHES = (
    BranchDefinition(NOT_FOUND, "The requested resource does not exist or is inaccessible.", True),
    BranchDefinition(PROVIDER_REJECTED, "The provider rejected the request.", True),
    BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
    BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
)
GET_PROJECT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_PROJECT_OPERATION_ID),
    (BranchDefinition(FOUND, "Read one GitLab project by numeric ID or namespace path."), *_SHARED_BRANCHES),
    _STEP_DEFAULTS,
)
LIST_ISSUES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_ISSUES_OPERATION_ID),
    (BranchDefinition(LISTED, "List the first page of issues for a GitLab project."), *_SHARED_BRANCHES),
    _STEP_DEFAULTS,
)
UPDATE_ISSUE_LABELS_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_ISSUE_LABELS_OPERATION_ID),
    (
        BranchDefinition(
            UPDATED, "Replace one GitLab issue's complete label set; repeating the request is safe."
        ),
        *_SHARED_BRANCHES,
    ),
    _STEP_DEFAULTS,
)

Output = TypeVar("Output")


@dataclass(frozen=True, slots=True)
class Config:
    """GitLab HTTP settings; zero or blank values use manifest defaults."""

    base_url: str = _DEFAULT_BASE_URL
    timeout_seconds: float = _DEFAULT_TIMEOUT_SECONDS
    max_response_bytes: int = _DEFAULT_MAX_RESPONSE_BYTES


@dataclass(frozen=True, slots=True)
class Credentials:
    """GitLab personal, project, or group access token, resolved anew for every provider attempt."""

    access_token: SecretString


@dataclass(frozen=True, slots=True)
class _Outcome:
    """A terminal or retry attempt whose empty value the calling operation supplies."""

    branch: str
    failure: Failure
    is_retry: bool = False
    retry_after_seconds: float = 0

    def as_query(self, empty: Output) -> QueryAttempt[Output]:
        if self.is_retry:
            return QueryAttempt.retry(self.failure, self.retry_after_seconds)
        return QueryAttempt.branch_result(self.branch, empty, Receipt(), self.failure)

    def as_mutation(self, empty: Output) -> MutationAttempt[Output]:
        if self.is_retry:
            return MutationAttempt.retry(self.failure, self.retry_after_seconds)
        return MutationAttempt.branch_result(self.branch, empty, Receipt(), self.failure)


@dataclass(frozen=True, slots=True)
class _Success:
    """A decoded happy-branch value."""

    value: Any


class Client:
    """One redirect-safe HTTP client and call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None:
            raise ValueError("GitLab configuration is required")
        if credentials is None:
            raise ValueError("GitLab credential source is required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("GitLab timeout and maxResponseBytes must be positive")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def get_project(self) -> "GetProjectOperation":
        """Bind the getProject Query to this client."""
        return GetProjectOperation(self)

    def list_issues(self) -> "ListIssuesOperation":
        """Bind the listIssues Query to this client."""
        return ListIssuesOperation(self)

    def update_issue_labels(self) -> "UpdateIssueLabelsOperation":
        """Bind the updateIssueLabels Mutation to this client."""
        return UpdateIssueLabelsOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def execute(
        self,
        call: Call,
        operation: str,
        method: str,
        path: str,
        body: bytes | None,
        has_not_found: bool,
        decode: Callable[[Any], Any],
    ) -> _Success | _Outcome:
        """Send one request and classify it exactly like the Go executeQuery and executeMutation helpers."""
        try:
            credentials = await self.credentials.resolve(call)
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            return _outcome(
                DEFECT, operation, FailureKind.AUTHENTICATION, "connection credentials are unavailable"
            )
        token = credentials.access_token.reveal()
        if not token or not is_header_safe_credential(token):
            return _outcome(
                DEFECT, operation, FailureKind.AUTHENTICATION, "connection credentials are invalid"
            )
        headers = {"PRIVATE-TOKEN": token, "Accept": "application/json"}
        if body is not None:
            headers["Content-Type"] = "application/x-www-form-urlencoded"
        try:
            request = self.http_client.build_request(
                method, self.base_url + path, content=body, headers=headers
            )
        except (httpx.HTTPError, ValueError):
            return _outcome(DEFECT, operation, FailureKind.LOCAL_DEFECT, "request could not be built")
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _outcome("", operation, FailureKind.AVAILABILITY, "GitLab request failed", is_retry=True)
        try:
            status = response.status_code
            if status in (408, 409, 429) or status >= 500:
                kind = FailureKind.RATE_LIMIT if status == 429 else FailureKind.AVAILABILITY
                retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
                return _outcome("", operation, kind, f"GitLab returned HTTP {status}", True, retry_after)
            if status != 200:
                branch = NOT_FOUND if status == 404 and has_not_found else PROVIDER_REJECTED
                kind = _STATUS_KINDS.get(status, FailureKind.PROVIDER_REJECTION)
                return _outcome(branch, operation, kind, f"GitLab returned HTTP {status}")
            try:
                response_body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _outcome(
                    INVALID_RESPONSE,
                    operation,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _outcome(
                    "", operation, FailureKind.AVAILABILITY, "response was interrupted", is_retry=True
                )
            try:
                return _Success(decode(json.loads(response_body)))
            except (ValueError, TypeError, UnicodeDecodeError):
                return _outcome(INVALID_RESPONSE, operation, FailureKind.PROTOCOL, "response was malformed")
        finally:
            await response.aclose()


class GetProjectOperation:
    """Read one GitLab project by numeric ID or namespace path."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("GitLab client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return GET_PROJECT_DEFINITION

    async def invoke(self, call: Call, input: GetProjectInput) -> QueryAttempt[Project]:
        """Validate the project, read once, then select a branch or safe retry."""
        operation = GET_PROJECT_OPERATION_ID
        if not isinstance(input, GetProjectInput) or not _is_nonblank(input.project_id):
            return _query_failed(operation, Project(), "projectId is required")
        result = await self.client.execute(
            call, operation, "GET", _project_path(input.project_id), None, True, _decode_project
        )
        if isinstance(result, _Outcome):
            return result.as_query(Project())
        return QueryAttempt.branch_result(FOUND, result.value, Receipt())


class ListIssuesOperation:
    """List the first page of issues for a GitLab project."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("GitLab client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return LIST_ISSUES_DEFINITION

    async def invoke(self, call: Call, input: ListIssuesInput) -> QueryAttempt[IssuesPage]:
        """Validate the project, list once, then select a branch or safe retry."""
        operation = LIST_ISSUES_OPERATION_ID
        if not isinstance(input, ListIssuesInput) or not _is_nonblank(input.project_id):
            return _query_failed(operation, IssuesPage(), "projectId is required")
        result = await self.client.execute(
            call, operation, "GET", _project_path(input.project_id) + "/issues", None, True, _decode_issues
        )
        if isinstance(result, _Outcome):
            return result.as_query(IssuesPage())
        return QueryAttempt.branch_result(LISTED, result.value, Receipt())


class UpdateIssueLabelsOperation:
    """Replace one issue's complete label set; repeating the request is safe."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("GitLab client is required")
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return UPDATE_ISSUE_LABELS_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpdateIssueLabelsInput) -> IdempotencyKey:
        """Use the stable call ID; this operation sets an absolute state."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: UpdateIssueLabelsInput) -> MutationAttempt[Issue]:
        """Validate, replace the labels once, then select a branch or safe retry."""
        operation = UPDATE_ISSUE_LABELS_OPERATION_ID
        message = _label_update_validation_message(input)
        if message:
            return MutationAttempt.branch_result(
                DEFECT, Issue(), Receipt(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, operation, message)
            )
        body = urlencode({"labels": ",".join(input.labels)}).encode()
        path = f"{_project_path(input.project_id)}/issues/{input.issue_iid}"
        result = await self.client.execute(call, operation, "PUT", path, body, True, _decode_updated_issue)
        if isinstance(result, _Outcome):
            return result.as_mutation(Issue())
        return MutationAttempt.branch_result(UPDATED, result.value, Receipt())


def _label_update_validation_message(input: UpdateIssueLabelsInput) -> str:
    if (
        not isinstance(input, UpdateIssueLabelsInput)
        or not _is_nonblank(input.project_id)
        or type(input.issue_iid) is not int
        or input.issue_iid <= 0
    ):
        return "projectId and positive issueIid are required"
    if any(not _is_nonblank(label) or "," in label for label in input.labels):
        return "labels must be nonblank and cannot contain commas"
    return ""


def _project_path(project_id: str) -> str:
    return "/projects/" + quote(project_id, safe=_PATH_SEGMENT_SAFE)


def _is_nonblank(value: object) -> bool:
    return isinstance(value, str) and bool(value.strip())


def _decode_project(document: Any) -> Project:
    document = _object(document)
    project_id = _optional(document, "id", 0)
    fields = tuple(_optional(document, name, "") for name in ("name", "path_with_namespace", "web_url"))
    if type(project_id) is not int or project_id <= 0 or not all(isinstance(value, str) for value in fields):
        raise ValueError
    return Project(project_id, *fields)


def _decode_issues(document: Any) -> IssuesPage:
    if not isinstance(document, list):
        raise ValueError
    return IssuesPage(tuple(_decode_issue(issue) for issue in document))


def _decode_updated_issue(document: Any) -> Issue:
    issue = _decode_issue(document)
    if issue.id <= 0 or issue.iid <= 0:
        raise ValueError
    return issue


def _decode_issue(document: Any) -> Issue:
    document = _object(document)
    issue_id = _optional(document, "id", 0)
    issue_iid = _optional(document, "iid", 0)
    title = _optional(document, "title", "")
    labels = _optional(document, "labels", [])
    web_url = _optional(document, "web_url", "")
    if (
        type(issue_id) is not int
        or type(issue_iid) is not int
        or not isinstance(title, str)
        or not isinstance(web_url, str)
        or not isinstance(labels, list)
    ):
        raise ValueError
    return Issue(issue_id, issue_iid, title, tuple(_label(label) for label in labels), web_url)


def _label(value: Any) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError
    return value


def _object(value: Any) -> dict[str, Any]:
    """Return a JSON object, treating null like Go's zero struct."""
    if value is None:
        return {}
    if not isinstance(value, dict):
        raise ValueError
    return value


def _optional(document: dict[str, Any], name: str, default: Any) -> Any:
    """Return a JSON field, treating absent or null like Go's zero value."""
    value = document.get(name)
    return default if value is None else value


def _query_failed(operation: str, value: Output, message: str) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(
        DEFECT, value, Receipt(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, operation, message)
    )


def _outcome(
    branch: str,
    operation: str,
    kind: FailureKind,
    message: str,
    is_retry: bool = False,
    retry_after_seconds: float = 0,
) -> _Outcome:
    return _Outcome(branch, Failure(kind, CONNECTOR_ID, operation, message), is_retry, retry_after_seconds)
