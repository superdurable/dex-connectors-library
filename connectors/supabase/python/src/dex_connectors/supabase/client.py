# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Supabase PostgREST row selection and upserts with Go-compatible branch classification."""

import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from datetime import timedelta as timedelta
from typing import Any, TypeVar
from urllib.parse import urlencode

import httpx
from dex_connectors.sdk import BranchDefinition as BranchDefinition
from dex_connectors.sdk import (
    Call,
    CallID,
    CredentialProvider,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    QueryAttempt,
    QueryDefinition,
    ReauthorizationRequiredError,
    Receipt,
)
from dex_connectors.sdk import OperationRef as OperationRef
from dex_connectors.sdk import SecretString as SecretString
from dex_connectors.sdk import StepDefaults as StepDefaults
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
from dex_connectors.sdk.retry_policy import (
    default_connector_step_execute_retry_policy as default_connector_step_execute_retry_policy,
)

from ._generated.config import (
    Config as Config,
)
from ._generated.config import (
    Credentials as Credentials,
)
from ._generated.definitions import (
    CONNECTOR_ID as CONNECTOR_ID,
)
from ._generated.definitions import (
    DEFECT as DEFECT,
)
from ._generated.definitions import (
    INVALID_RESPONSE as INVALID_RESPONSE,
)
from ._generated.definitions import (
    PROVIDER_REJECTED as PROVIDER_REJECTED,
)
from ._generated.definitions import (
    SELECT_ROWS_DEFINITION as SELECT_ROWS_DEFINITION,
)
from ._generated.definitions import (
    SELECT_ROWS_OPERATION_ID as SELECT_ROWS_OPERATION_ID,
)
from ._generated.definitions import (
    SELECTED as SELECTED,
)
from ._generated.definitions import (
    UPSERT_ROWS_DEFINITION as UPSERT_ROWS_DEFINITION,
)
from ._generated.definitions import (
    UPSERT_ROWS_OPERATION_ID as UPSERT_ROWS_OPERATION_ID,
)
from ._generated.definitions import (
    UPSERTED as UPSERTED,
)
from .models import SelectRowsInput, SelectRowsOutput, UpsertRowsInput, UpsertRowsOutput

UPSERT_PREFERENCE = "resolution=merge-duplicates,return=representation"
_IDENTIFIER = re.compile(r"[A-Za-z0-9_-]+")
_COLUMN_LIST = re.compile(r"[A-Za-z_][A-Za-z0-9_]*(,[A-Za-z_][A-Za-z0-9_]*)*")
_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 2_097_152
_DEFAULT_SELECT_LIMIT = 100
_MAX_SELECT_LIMIT = 1000
_MAX_UPSERT_ROWS = 100
_STATUS_KINDS = {
    401: FailureKind.AUTHENTICATION,
    403: FailureKind.AUTHORIZATION,
    400: FailureKind.VALIDATION,
    422: FailureKind.VALIDATION,
}

Output = TypeVar("Output")






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


class Client:
    """One redirect-safe HTTP client bound to one project's Data API, plus a call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None:
            raise ValueError("supabase configuration is required")
        if credentials is None:
            raise ValueError("supabase credential source is required")
        if not config.base_url:
            raise ValueError("configuration baseUrl is required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("supabase timeout and maxResponseBytes must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def select_rows(self) -> "SelectRowsOperation":
        """Bind the selectRows Query to this client."""
        return SelectRowsOperation(self)

    def upsert_rows(self) -> "UpsertRowsOperation":
        """Bind the upsertRows Mutation to this client."""
        return UpsertRowsOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def send(
        self,
        call: Call,
        operation: str,
        method: str,
        path: str,
        query: dict[str, str],
        body: bytes | None,
        success_statuses: tuple[int, ...],
    ) -> Any:
        """Send one request and classify it exactly like the Go send helper.

        Returns the decoded JSON document on success, or an _Outcome.
        """
        try:
            credentials = await self.credentials.resolve(call)
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            return _outcome(
                DEFECT, operation, FailureKind.AUTHENTICATION, "connection credentials are unavailable"
            )
        token = credentials.api_key.reveal()
        if not token or not is_header_safe_credential(token):
            return _outcome(DEFECT, operation, FailureKind.AUTHENTICATION, "connection credential is invalid")
        address = self.base_url + path
        if query:
            address += "?" + urlencode(sorted(query.items()))
        headers = {"Accept": "application/json", "User-Agent": "superdurable-supabase/0.1", "apikey": token}
        if method == "POST":
            headers["Prefer"] = UPSERT_PREFERENCE
        if body is not None:
            headers["Content-Type"] = "application/json"
        try:
            request = self.http_client.build_request(method, address, content=body, headers=headers)
        except (httpx.HTTPError, ValueError):
            return _outcome(DEFECT, operation, FailureKind.LOCAL_DEFECT, "request could not be built")
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _outcome("", operation, FailureKind.AVAILABILITY, "provider request failed", is_retry=True)
        try:
            status = response.status_code
            if status in (408, 429) or status >= 500:
                kind = FailureKind.RATE_LIMIT if status == 429 else FailureKind.AVAILABILITY
                retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
                return _outcome("", operation, kind, "provider is temporarily unavailable", True, retry_after)
            if status not in success_statuses:
                kind = _STATUS_KINDS.get(status, FailureKind.PROVIDER_REJECTION)
                return _outcome(
                    PROVIDER_REJECTED, operation, kind, f"provider rejected the request with HTTP {status}"
                )
            try:
                response_body = await read_bounded_body(response, self.max_response_bytes)
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
                return json.loads(response_body)
            except (ValueError, UnicodeDecodeError):
                return _outcome(
                    INVALID_RESPONSE, operation, FailureKind.PROTOCOL, "provider returned malformed JSON"
                )
        finally:
            await response.aclose()


class SelectRowsOperation:
    """Select a bounded page of table rows through PostgREST."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Supabase client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return SELECT_ROWS_DEFINITION

    async def invoke(self, call: Call, input: SelectRowsInput) -> QueryAttempt[SelectRowsOutput]:
        """Validate, read once, then select a branch or safe retry."""
        operation = SELECT_ROWS_OPERATION_ID
        query = _encode_select_rows_query(input)
        if isinstance(query, str):
            return _query_failed(DEFECT, FailureKind.VALIDATION, query)
        response = await self.client.send(call, operation, "GET", "/" + input.table, query, None, (200,))
        if isinstance(response, _Outcome):
            return response.as_query(SelectRowsOutput())
        if not isinstance(response, list):
            return _query_failed(
                INVALID_RESPONSE, FailureKind.PROTOCOL, "provider returned an incomplete response"
            )
        return QueryAttempt.branch_result(SELECTED, SelectRowsOutput(response), Receipt())


class UpsertRowsOperation:
    """Upsert rows with a declared unique conflict key and merge duplicates."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Supabase client is required")
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return UPSERT_ROWS_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpsertRowsInput) -> IdempotencyKey:
        """Use the stable Dex call identity; the provider write is an upsert by a unique conflict key."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: UpsertRowsInput) -> MutationAttempt[UpsertRowsOutput]:
        """Validate, upsert once, then select a branch or safe retry."""
        operation = UPSERT_ROWS_OPERATION_ID
        body = _encode_upsert_rows_body(input)
        if isinstance(body, str):
            return _mutation_failed(DEFECT, FailureKind.VALIDATION, body)
        response = await self.client.send(
            call, operation, "POST", "/" + input.table, {"on_conflict": input.on_conflict}, body, (201, 200)
        )
        if isinstance(response, _Outcome):
            return response.as_mutation(UpsertRowsOutput())
        if not isinstance(response, list):
            return _mutation_failed(
                INVALID_RESPONSE, FailureKind.PROTOCOL, "provider returned an incomplete response"
            )
        return MutationAttempt.branch_result(UPSERTED, UpsertRowsOutput(response), Receipt())


def _encode_select_rows_query(input: SelectRowsInput) -> dict[str, str] | str:
    if (
        not isinstance(input, SelectRowsInput)
        or not _is_identifier(input.table)
        or type(input.limit) is not int
        or type(input.offset) is not int
        or not 0 <= input.limit <= _MAX_SELECT_LIMIT
        or input.offset < 0
    ):
        return "invalid table, limit, or offset"
    if input.columns and not _is_column_list(input.columns):
        return "columns must be comma-separated identifiers"
    query = {"select": input.columns or "*", "limit": str(input.limit or _DEFAULT_SELECT_LIMIT)}
    if input.offset > 0:
        query["offset"] = str(input.offset)
    return query


def _encode_upsert_rows_body(input: UpsertRowsInput) -> bytes | str:
    if (
        not isinstance(input, UpsertRowsInput)
        or not _is_identifier(input.table)
        or not _is_column_list(input.on_conflict)
        or not 1 <= len(input.rows) <= _MAX_UPSERT_ROWS
    ):
        return "table, unique onConflict, and 1 through 100 rows are required"
    conflict_columns = input.on_conflict.split(",")
    seen_keys: set[str] = set()
    for row in input.rows:
        if not isinstance(row, dict) or not row:
            return "rows must not be empty"
        if any(row.get(column) is None for column in conflict_columns):
            return "every row must contain non-null onConflict columns"
        try:
            key = _dump_json([_as_go_number(row[column]) for column in conflict_columns])
        except (TypeError, ValueError):
            return "onConflict values must be JSON serializable"
        if key in seen_keys:
            return "rows contain duplicate onConflict values"
        seen_keys.add(key)
    try:
        return _dump_json(list(input.rows)).encode()
    except (TypeError, ValueError):
        return "rows must be JSON serializable"


def _as_go_number(value: object) -> object:
    """Compare conflict keys as Go does, where every JSON number is a float64."""
    if isinstance(value, bool) or not isinstance(value, int | float):
        return value
    return float(value)


def _dump_json(value: object) -> str:
    """Encode like Go json.Marshal of decoded JSON: sorted object keys and no NaN or Infinity."""
    return json.dumps(value, separators=(",", ":"), sort_keys=True, allow_nan=False)


def _is_identifier(value: object) -> bool:
    return isinstance(value, str) and _IDENTIFIER.fullmatch(value) is not None


def _is_column_list(value: object) -> bool:
    return isinstance(value, str) and _COLUMN_LIST.fullmatch(value) is not None


def _outcome(
    branch: str,
    operation: str,
    kind: FailureKind,
    message: str,
    is_retry: bool = False,
    retry_after_seconds: float = 0,
) -> _Outcome:
    return _Outcome(branch, Failure(kind, CONNECTOR_ID, operation, message), is_retry, retry_after_seconds)


def _query_failed(branch: str, kind: FailureKind, message: str) -> QueryAttempt[SelectRowsOutput]:
    return QueryAttempt.branch_result(
        branch, SelectRowsOutput(), Receipt(), Failure(kind, CONNECTOR_ID, SELECT_ROWS_OPERATION_ID, message)
    )


def _mutation_failed(branch: str, kind: FailureKind, message: str) -> MutationAttempt[UpsertRowsOutput]:
    return MutationAttempt.branch_result(
        branch, UpsertRowsOutput(), Receipt(), Failure(kind, CONNECTOR_ID, UPSERT_ROWS_OPERATION_ID, message)
    )
