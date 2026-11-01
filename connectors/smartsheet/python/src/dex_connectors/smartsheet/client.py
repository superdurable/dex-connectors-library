# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Smartsheet provider reads and Go-compatible branch classification."""

import json
from collections.abc import Callable
from datetime import UTC, datetime, timedelta
from typing import Any, TypeVar

import httpx
from dex_connectors.sdk import (
    BranchDefinition,
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    QueryAttempt,
    QueryDefinition,
    ReauthorizationRequiredError,
    Receipt,
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
    FOUND as FOUND,
)
from ._generated.definitions import (
    GET_SHEET_DEFINITION as GET_SHEET_DEFINITION,
)
from ._generated.definitions import (
    GET_SHEET_OPERATION_ID as GET_SHEET_OPERATION_ID,
)
from ._generated.definitions import (
    INVALID_RESPONSE as INVALID_RESPONSE,
)
from ._generated.definitions import (
    LIST_SHEETS_DEFINITION as LIST_SHEETS_DEFINITION,
)
from ._generated.definitions import (
    LIST_SHEETS_OPERATION_ID as LIST_SHEETS_OPERATION_ID,
)
from ._generated.definitions import (
    LISTED as LISTED,
)
from ._generated.definitions import (
    NOT_FOUND as NOT_FOUND,
)
from ._generated.definitions import (
    PROVIDER_REJECTED as PROVIDER_REJECTED,
)
from .models import GetSheetInput, ListSheetsInput, Sheet, SheetColumn, SheetPage, SheetRow, SheetSummary

_DEFAULT_BASE_URL = "https://api.smartsheet.com/2.0"
_DEFAULT_TIMEOUT_SECONDS = 30
_DEFAULT_MAX_RESPONSE_BYTES = 4_194_304
_STEP_DEFAULTS = StepDefaults(
    timedelta(seconds=45), 2, execute_retry=default_connector_step_execute_retry_policy()
)
_SHARED_BRANCHES = (
    BranchDefinition(PROVIDER_REJECTED, "Smartsheet conclusively rejected the read.", True),
    BranchDefinition(INVALID_RESPONSE, "Smartsheet returned malformed or oversized JSON.", True),
    BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
)

Output = TypeVar("Output")






class Client:
    """One redirect-safe HTTP client and call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None:
            raise ValueError("Smartsheet configuration is required")
        if credentials is None:
            raise ValueError("Smartsheet credential source is required")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("Smartsheet timeout and max response bytes must be positive")
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def list_sheets(self) -> "ListSheetsOperation":
        """Bind the sheet page Query to this client."""
        return ListSheetsOperation(self)

    def get_sheet(self) -> "GetSheetOperation":
        """Bind the sheet lookup Query to this client."""
        return GetSheetOperation(self)

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


class ListSheetsOperation:
    """Read one page of Smartsheet sheets."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Smartsheet client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return LIST_SHEETS_DEFINITION

    async def invoke(self, call: Call, input: ListSheetsInput) -> QueryAttempt[SheetPage]:
        """Validate the page, read once, then select a branch or safe retry."""
        operation = LIST_SHEETS_OPERATION_ID
        if (
            not isinstance(input, ListSheetsInput)
            or type(input.page) is not int
            or type(input.page_size) is not int
            or input.page < 0
            or not 0 <= input.page_size <= 1000
        ):
            return _failed(
                operation, DEFECT, SheetPage(), FailureKind.VALIDATION, "page or pageSize is invalid"
            )
        path = f"/sheets?page={max(input.page, 1)}&pageSize={input.page_size or 100}"
        return await self.client.invoke_get(
            call, operation, path, SheetPage(), LISTED, PROVIDER_REJECTED, _decode_sheet_page
        )


class GetSheetOperation:
    """Read one Smartsheet sheet with its columns and rows."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Smartsheet client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return GET_SHEET_DEFINITION

    async def invoke(self, call: Call, input: GetSheetInput) -> QueryAttempt[Sheet]:
        """Validate the sheet ID, read once, then select a branch or safe retry."""
        operation = GET_SHEET_OPERATION_ID
        if not isinstance(input, GetSheetInput) or type(input.sheet_id) is not int or input.sheet_id <= 0:
            return _failed(operation, DEFECT, Sheet(), FailureKind.VALIDATION, "sheetId must be positive")
        return await self.client.invoke_get(
            call, operation, f"/sheets/{input.sheet_id}", Sheet(), FOUND, NOT_FOUND, _decode_sheet
        )


def _failed(
    operation: str, branch: str, value: Output, kind: FailureKind, message: str
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(
        branch, value, Receipt(), Failure(kind, CONNECTOR_ID, operation, message)
    )


def _retry(operation: str, kind: FailureKind, message: str, after_seconds: float) -> QueryAttempt[Any]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, operation, message), after_seconds)


def _decode_sheet_page(document: Any) -> SheetPage:
    if not isinstance(document, dict) or not isinstance(document.get("data"), list):
        raise ValueError
    page = SheetPage(
        tuple(
            SheetSummary(_integer(item, "id"), _string(item, "name"))
            for item in map(_object, document["data"])
        ),
        _integer(document, "pageNumber"),
        _integer(document, "totalCount"),
    )
    if page.page_number < 1 or page.total_count < 0:
        raise ValueError
    return page


def _decode_sheet(document: Any) -> Sheet:
    document = _object(document)
    columns = document.get("columns")
    rows = document.get("rows")
    if not isinstance(columns, list) or not isinstance(rows, list):
        raise ValueError
    sheet = Sheet(
        _integer(document, "id"),
        _string(document, "name"),
        tuple(
            SheetColumn(_integer(column, "id"), _string(column, "title")) for column in map(_object, columns)
        ),
        tuple(_decode_row(row) for row in map(_object, rows)),
    )
    if sheet.id <= 0 or not sheet.name:
        raise ValueError
    return sheet


def _decode_row(row: dict[str, Any]) -> SheetRow:
    cells = row.get("cells")
    if cells is None:
        cells = []
    if not isinstance(cells, list):
        raise ValueError
    return SheetRow(_integer(row, "id"), tuple(cells))


def _object(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError
    return value


def _integer(document: dict[str, Any], name: str) -> int:
    value = document.get(name)
    if value is None:
        return 0
    if type(value) is not int:
        raise ValueError
    return value


def _string(document: dict[str, Any], name: str) -> str:
    value = document.get(name)
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError
    return value
