# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Coda provider reads and Go-compatible branch classification."""

import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any, TypeVar
from urllib.parse import urlencode

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

from .models import GetRowInput, GetRowOutput, ListRowsInput, ListRowsOutput

CONNECTOR_ID = "coda"
LIST_ROWS_OPERATION_ID = "listRows"
GET_ROW_OPERATION_ID = "getRow"
FOUND = "found"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
INVALID_RESPONSE = "invalidResponse"
DEFECT = "defect"
_IDENTIFIER = re.compile(r"[A-Za-z0-9_-]+")
_DEFAULT_BASE_URL = "https://coda.io/apis/v1"
_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 2097152
_STEP_DEFAULTS = StepDefaults(
    timedelta(seconds=30), 2, execute_retry=default_connector_step_execute_retry_policy()
)
_STATUS_KINDS = {
    401: FailureKind.AUTHENTICATION,
    403: FailureKind.AUTHORIZATION,
    400: FailureKind.VALIDATION,
    422: FailureKind.VALIDATION,
}
LIST_ROWS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, "listRows"),
    (
        BranchDefinition("listed", "List one page of rows in a Coda table."),
        BranchDefinition(
            "providerRejected", "The provider conclusively rejected the request or credentials.", True
        ),
        BranchDefinition("invalidResponse", "The provider returned malformed or oversized JSON.", True),
        BranchDefinition("defect", "Local input, configuration, or credentials are invalid.", True),
    ),
    _STEP_DEFAULTS,
)
GET_ROW_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, "getRow"),
    (
        BranchDefinition("found", "Get one row in a Coda table."),
        BranchDefinition(
            "notFound", "The requested provider resource does not exist or is not visible.", True
        ),
        BranchDefinition(
            "providerRejected", "The provider conclusively rejected the request or credentials.", True
        ),
        BranchDefinition("invalidResponse", "The provider returned malformed or oversized JSON.", True),
        BranchDefinition("defect", "Local input, configuration, or credentials are invalid.", True),
    ),
    _STEP_DEFAULTS,
)

Output = TypeVar("Output")


@dataclass(frozen=True, slots=True)
class Config:
    """Coda HTTP settings; zero or blank values use manifest defaults."""

    base_url: str = _DEFAULT_BASE_URL
    timeout_seconds: float = _DEFAULT_TIMEOUT_SECONDS
    max_response_bytes: int = _DEFAULT_MAX_RESPONSE_BYTES


@dataclass(frozen=True, slots=True)
class Credentials:
    """Coda API token, resolved anew for every provider attempt."""

    api_token: SecretString


@dataclass(frozen=True, slots=True)
class _Outcome:
    """A terminal or retry attempt whose empty value the calling operation supplies."""

    branch: str
    failure: Failure
    is_retry: bool = False
    retry_after_seconds: float = 0

    def with_empty(self, empty: Output) -> QueryAttempt[Output]:
        if self.is_retry:
            return QueryAttempt.retry(self.failure, self.retry_after_seconds)
        return QueryAttempt.branch_result(self.branch, empty, Receipt(), self.failure)


class Client:
    """One redirect-safe HTTP client and call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None:
            raise ValueError("coda configuration is required")
        if credentials is None:
            raise ValueError("coda credential source is required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("coda timeout and maxResponseBytes must be positive")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL).rstrip("/")
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def list_rows(self) -> "ListRowsOperation":
        """Bind the listRows Query to this client."""
        return ListRowsOperation(self)

    def get_row(self) -> "GetRowOperation":
        """Bind the getRow Query to this client."""
        return GetRowOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def send(
        self, call: Call, operation: str, path: str, query: dict[str, str], has_not_found: bool
    ) -> Any:
        """Send one GET and classify it exactly like the Go send helper.

        Returns the decoded JSON document on success, or an _Outcome.
        """
        try:
            credentials = await self.credentials.resolve(call)
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            return _outcome(
                DEFECT, operation, FailureKind.AUTHENTICATION, "connection credentials are unavailable"
            )
        token = credentials.api_token.reveal()
        if not token or not is_header_safe_credential(token):
            return _outcome(DEFECT, operation, FailureKind.AUTHENTICATION, "connection credential is invalid")
        address = self.base_url + path
        if query:
            address += "?" + urlencode(sorted(query.items()))
        try:
            request = self.http_client.build_request(
                "GET",
                address,
                headers={
                    "Accept": "application/json",
                    "User-Agent": "superdurable-coda/0.1",
                    "Authorization": "Bearer " + token,
                },
            )
        except (httpx.HTTPError, ValueError):
            return _outcome(DEFECT, operation, FailureKind.LOCAL_DEFECT, "request could not be built")
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _outcome("", operation, FailureKind.AVAILABILITY, "provider request failed", is_retry=True)
        try:
            status = response.status_code
            if status in (408, 409, 429) or status >= 500:
                kind = FailureKind.RATE_LIMIT if status == 429 else FailureKind.AVAILABILITY
                retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
                return _outcome("", operation, kind, "provider is temporarily unavailable", True, retry_after)
            if status == 404 and has_not_found:
                return _outcome(NOT_FOUND, operation, FailureKind.NOT_FOUND, "resource was not found")
            if status != 200:
                kind = _STATUS_KINDS.get(status, FailureKind.PROVIDER_REJECTION)
                return _outcome(
                    PROVIDER_REJECTED, operation, kind, f"provider rejected the request with HTTP {status}"
                )
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _outcome(
                    INVALID_RESPONSE,
                    operation,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "provider response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _outcome(
                    "",
                    operation,
                    FailureKind.AVAILABILITY,
                    "provider response was interrupted",
                    is_retry=True,
                )
            try:
                return json.loads(body)
            except (ValueError, UnicodeDecodeError):
                return _outcome(
                    INVALID_RESPONSE, operation, FailureKind.PROTOCOL, "provider returned malformed JSON"
                )
        finally:
            await response.aclose()


class ListRowsOperation:
    """List one page of rows in a Coda table."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Coda client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return LIST_ROWS_DEFINITION

    async def invoke(self, call: Call, input: ListRowsInput) -> QueryAttempt[ListRowsOutput]:
        """Validate, read once, then select a branch or safe retry."""
        operation = "listRows"
        if not isinstance(input, ListRowsInput) or not (
            _is_id(input.doc_id)
            and _is_id(input.table_id)
            and type(input.limit) is int
            and 0 <= input.limit <= 500
            and isinstance(input.page_token, str)
        ):
            return _failed(
                operation, DEFECT, ListRowsOutput(), FailureKind.VALIDATION, "invalid doc, table, or limit"
            )
        query: dict[str, str] = {}
        if input.limit > 0:
            query["limit"] = str(input.limit)
        if input.page_token:
            query["pageToken"] = input.page_token
        response = await self.client.send(
            call, operation, f"/docs/{input.doc_id}/tables/{input.table_id}/rows", query, False
        )
        if isinstance(response, _Outcome):
            return response.with_empty(ListRowsOutput())
        if not has_response_shape(response, "items"):
            return _failed(
                operation,
                INVALID_RESPONSE,
                ListRowsOutput(),
                FailureKind.PROTOCOL,
                "provider returned an incomplete response",
            )
        return QueryAttempt.branch_result("listed", ListRowsOutput(response), Receipt())


class GetRowOperation:
    """Get one row in a Coda table."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Coda client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return GET_ROW_DEFINITION

    async def invoke(self, call: Call, input: GetRowInput) -> QueryAttempt[GetRowOutput]:
        """Validate, read once, then select a branch or safe retry."""
        operation = "getRow"
        if not isinstance(input, GetRowInput) or not (
            _is_id(input.doc_id) and _is_id(input.table_id) and _is_id(input.row_id)
        ):
            return _failed(
                operation,
                DEFECT,
                GetRowOutput(),
                FailureKind.VALIDATION,
                "doc, table, and row IDs are required",
            )
        query: dict[str, str] = {}
        response = await self.client.send(
            call, operation, f"/docs/{input.doc_id}/tables/{input.table_id}/rows/{input.row_id}", query, True
        )
        if isinstance(response, _Outcome):
            return response.with_empty(GetRowOutput())
        if not has_response_shape(response, "id"):
            return _failed(
                operation,
                INVALID_RESPONSE,
                GetRowOutput(),
                FailureKind.PROTOCOL,
                "provider returned an incomplete response",
            )
        return QueryAttempt.branch_result("found", GetRowOutput(response), Receipt())


def has_response_shape(document: Any, key: str) -> bool:
    """Apply the Go hasResponseShape check to a decoded JSON document."""
    if key == "array":
        return isinstance(document, list)
    if not isinstance(document, dict) or document.get(key) is None:
        return False
    value = document[key]
    if key in ("items", "matches"):
        return isinstance(value, list)
    if key == "id":
        return isinstance(value, str) and value != ""
    if key == "sys":
        return isinstance(value, dict)
    return True


def _outcome(
    branch: str,
    operation: str,
    kind: FailureKind,
    message: str,
    is_retry: bool = False,
    retry_after_seconds: float = 0,
) -> _Outcome:
    return _Outcome(branch, Failure(kind, CONNECTOR_ID, operation, message), is_retry, retry_after_seconds)


def _failed(
    operation: str, branch: str, value: Output, kind: FailureKind, message: str
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(
        branch, value, Receipt(), Failure(kind, CONNECTOR_ID, operation, message)
    )


def _is_id(value: object) -> bool:
    return isinstance(value, str) and _IDENTIFIER.fullmatch(value) is not None
