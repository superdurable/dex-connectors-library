# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Microsoft Excel Graph operations and Go-compatible outcome classification."""

from __future__ import annotations

import json
import math
import re
from dataclasses import dataclass, field
from datetime import datetime
from typing import Any

from dex_connectors.sdk import (
    Call,
    CallID,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    QueryAttempt,
    QueryDefinition,
    Receipt,
)
from dex_connectors.sdk.providerhttp import parse_retry_after

from ._generated.definitions import (
    APPEND_TABLE_ROWS_DEFINITION,
    GET_TABLE_ROWS_DEFINITION,
    GET_VALUES_DEFINITION,
    UPDATE_VALUES_DEFINITION,
)
from .client import (
    Client,
    GraphResponse,
    GraphTransportError,
    compact_json,
    path_escape,
    range_url,
    table_url,
)
from .models import (
    AppendTableRowsInput,
    AppendTableRowsOutput,
    GetTableRowsInput,
    GetTableRowsOutput,
    GetValuesInput,
    GetValuesOutput,
    TableRow,
    UpdateValuesInput,
    UpdateValuesOutput,
)

_DRIVE_ID = re.compile(r"^[A-Za-z0-9!_.~-]{1,512}$")
_WORKSHEET_ID = re.compile(r"^\{[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}\}$")
_A1_CELL = re.compile(r"^([A-Z]{1,3})([1-9][0-9]{0,6})$")
_ERROR_CODES = {
    "accessconflict": "retry",
    "gatewaytimeoutuncategorized": "retry",
    "serviceunavailableuncategorized": "retry",
    "toomanyrequestsuncategorized": "retry",
    "transientfailure": "retry",
    "activitylimitreached": "retry",
    "notfounduncategorized": "notFound",
    "itemnotfound": "notFound",
    "payloadtoolargeuncategorized": "tooLarge",
    "rangeexceedslimit": "tooLarge",
}
_QUERY_SELECT_COLUMNS = "?$select=id,index,name"
_QUERY_SELECT_VALUES = "?$select=address,rowCount,columnCount,values"
_QUERY_SELECT_TEXTS = "?$select=address,rowCount,columnCount,values,text"


@dataclass(frozen=True, slots=True)
class _DispatchMarker:
    dispatched_call_id: str = field(metadata={"dex_json_name": "microsoftExcelDispatchedCallId"})


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "microsoft-excel", operation, message)


def _receipt(client: Client, call: Call, response: GraphResponse | None = None) -> Receipt:
    return Receipt(
        call.id,
        "microsoft-excel",
        client.now(),
        call.idempotency_key,
        "",
        response.request_id if response else "",
    )


def _check_reference(text: str, name: str) -> None:
    if (
        not isinstance(text, str)
        or not text
        or len(text) > 255
        or not text.strip()
        or "/" in text
        or any(ord(char) < 32 or 127 <= ord(char) <= 159 for char in text)
    ):
        raise ValueError(f"{name} must be 1 to 255 characters without slashes or control characters")


def _check_location(drive_id: str, workbook_id: str) -> None:
    if not _DRIVE_ID.fullmatch(drive_id):
        raise ValueError("driveId must be a Microsoft Graph drive ID")
    if not _DRIVE_ID.fullmatch(workbook_id):
        raise ValueError("workbookId must be a Microsoft Graph drive item ID")


def _check_table(drive_id: str, workbook_id: str, table: str) -> None:
    _check_location(drive_id, workbook_id)
    _check_reference(table, "table")


def _check_worksheet(worksheet: str) -> None:
    if _WORKSHEET_ID.fullmatch(worksheet):
        return
    if (
        not worksheet
        or len(worksheet) > 31
        or any(char in "\\/?*[]:" for char in worksheet)
        or any(ord(char) < 32 or 127 <= ord(char) <= 159 for char in worksheet)
        or worksheet.startswith("'")
        or worksheet.endswith("'")
    ):
        raise ValueError("worksheet must be a worksheet ID or valid name")


def _a1_address(
    drive_id: str, workbook_id: str, worksheet: str, address: str, max_cells: int
) -> tuple[str, int, int]:
    _check_location(drive_id, workbook_id)
    _check_worksheet(worksheet)
    canonical = address.strip().upper()
    first, separator, last = canonical.partition(":")
    if not separator:
        last = first

    def parse(cell: str) -> tuple[int, int]:
        match = _A1_CELL.fullmatch(cell)
        if match is None:
            raise ValueError("address must be one cell or range in A1 form")
        column = 0
        for letter in match[1]:
            column = column * 26 + ord(letter) - 64
        row = int(match[2])
        if column > 16384 or row > 1048576:
            raise ValueError("address is outside Excel worksheet limits")
        return column, row

    first_column, first_row = parse(first)
    last_column, last_row = parse(last)
    if last_column < first_column or last_row < first_row:
        raise ValueError("address must name its top-left cell first")
    rows, columns = last_row - first_row + 1, last_column - first_column + 1
    if rows * columns > max_cells:
        raise ValueError("address covers more cells than the configured cell limit")
    return canonical, rows, columns


def _wire_cell(value: Any) -> Any:
    if isinstance(value, str):
        return "'" + value if value else ""
    if isinstance(value, bool):
        return value
    if type(value) in (int, float):
        try:
            if math.isfinite(value):
                return value
        except OverflowError:
            pass
    raise ValueError("a value is absent or is not a finite Excel cell value")


def _key_identity(value: Any) -> tuple[type, Any]:
    if type(value) in (int, float):
        return float, float(value)
    return type(value), value


def _matches_stored_key(key: Any, stored: Any) -> bool:
    if isinstance(key, str):
        return isinstance(stored, str) and stored in (key, "'" + key)
    return _key_identity(key) == _key_identity(stored)


def _decode_range(raw: bytes, text_required: bool) -> tuple[str, int, int, list[list[Any]], list[list[str]]]:
    try:
        body = json.loads(raw)
    except (ValueError, UnicodeError) as error:
        raise ValueError("invalid range JSON") from error
    if not isinstance(body, dict):
        raise ValueError("invalid range object")
    rows, columns = body.get("rowCount"), body.get("columnCount")
    if type(rows) is not int or type(columns) is not int or rows < 0 or columns < 0:
        raise ValueError("invalid range dimensions")
    values = body.get("values")
    if values is None:
        raise LookupError("Excel returned no cell values")
    if not isinstance(values, list) or len(values) != rows:
        raise ValueError("invalid range rows")
    for row in values:
        if (
            not isinstance(row, list)
            or len(row) != columns
            or any(
                not (cell is None or isinstance(cell, (str, bool)) or type(cell) in (int, float))
                for cell in row
            )
        ):
            raise ValueError("invalid range cells")
    text = body.get("text", [])
    if text_required and (not isinstance(text, list) or len(text) != rows):
        raise ValueError("invalid displayed text")
    if text_required and any(
        not isinstance(row, list) or len(row) != columns or any(not isinstance(cell, str) for cell in row)
        for row in text
    ):
        raise ValueError("invalid displayed text")
    address = body.get("address", "")
    if not isinstance(address, str):
        raise ValueError("invalid range address")
    return address, rows, columns, values, text


def _decode_columns(raw: bytes) -> list[tuple[str, str]]:
    try:
        body = json.loads(raw)
    except (ValueError, UnicodeError) as error:
        raise ValueError("invalid columns JSON") from error
    if not isinstance(body, dict) or not isinstance(body.get("value"), list) or not body["value"]:
        raise ValueError("invalid table columns")
    columns: list[tuple[int, str, str]] = []
    names: set[str] = set()
    for item in body["value"]:
        if not isinstance(item, dict):
            raise ValueError("invalid table column")
        index, name, identifier = item.get("index"), item.get("name"), item.get("id")
        if (
            type(index) is not int
            or not isinstance(name, str)
            or not name
            or name in names
            or not isinstance(identifier, str)
            or not identifier
        ):
            raise ValueError("invalid table column")
        names.add(name)
        columns.append((index, name, identifier))
    columns.sort()
    if [item[0] for item in columns] != list(range(len(columns))):
        raise ValueError("table column positions are not contiguous")
    return [(item[1], item[2]) for item in columns]


def _tokens(body: bytes) -> list[str]:
    try:
        value = json.loads(body)
        error = value.get("error", {})
        inner = error.get("innerError", error.get("innererror", {}))
        return [
            item
            for item in (inner.get("code"), error.get("code"))
            if isinstance(item, str) and re.fullmatch(r"[A-Za-z0-9_.:-]{1,64}", item)
        ]
    except (ValueError, AttributeError, TypeError):
        return []


def _error_class(status: int, tokens: list[str]) -> str:
    for token in tokens:
        if token.lower() in _ERROR_CODES:
            return _ERROR_CODES[token.lower()]
        if token.lower() in {
            "accessdenied",
            "badrequestuncategorized",
            "conflictuncategorized",
            "filteredrangeconflict",
            "forbiddenuncategorized",
            "generalexception",
            "insertdeleteconflict",
            "internalservererroruncategorized",
            "invalidargument",
            "invalidreference",
            "itemalreadyexists",
            "methodnotalloweduncategorized",
            "nonblankcelloffsheet",
            "notimplementeduncategorized",
            "requestaborted",
            "unauthorizeduncategorized",
            "unsupportedoperation",
            "unsupportedworkbook",
        }:
            return "rejected"
    if status in (408, 429, 500, 502, 503, 504):
        return "retry"
    if status == 404:
        return "notFound"
    if status == 413:
        return "tooLarge"
    return "rejected"


def _status_kind(status: int, tokens: list[str]) -> FailureKind:
    if status == 401:
        return FailureKind.AUTHENTICATION
    if status == 429 or any(
        token.lower() in ("toomanyrequestsuncategorized", "activitylimitreached") for token in tokens
    ):
        return FailureKind.RATE_LIMIT
    if status == 403:
        return FailureKind.AUTHORIZATION
    if status == 404:
        return FailureKind.NOT_FOUND
    if status == 409:
        return FailureKind.CONFLICT
    if status >= 500 or status == 408:
        return FailureKind.AVAILABILITY
    return FailureKind.PROVIDER_REJECTION


def _retry_after(response: GraphResponse, now: datetime) -> float:
    return parse_retry_after(response.headers.get("Retry-After", ""), now)


def _read_result(
    client: Client,
    call: Call,
    operation: str,
    response: GraphResponse | None,
    error: Exception | None,
    oversized_branch: str = "tooLarge",
) -> tuple[str, Failure | None, float, Receipt] | None:
    if isinstance(error, GraphTransportError):
        if not error.is_request_built:
            return "defect", _failure(operation, FailureKind.LOCAL_DEFECT, str(error)), 0, Receipt()
        return "retry", _failure(operation, FailureKind.TRANSPORT, "provider is unavailable"), 0, Receipt()
    if response is None:
        return "retry", _failure(operation, FailureKind.TRANSPORT, "provider is unavailable"), 0, Receipt()
    receipt = _receipt(client, call, response)
    if 200 <= response.status < 300:
        if response.is_body_too_large:
            return (
                oversized_branch,
                _failure(
                    operation,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "provider response exceeds the configured response limit",
                ),
                0,
                receipt,
            )
        return None
    tokens = _tokens(response.body)
    category = _error_class(response.status, tokens)
    kind = _status_kind(response.status, tokens)
    if category == "retry":
        return (
            "retry",
            _failure(operation, kind, "provider temporarily rejected the request"),
            _retry_after(response, client.now()),
            Receipt(),
        )
    if category == "notFound":
        return (
            "notFound",
            _failure(
                operation,
                FailureKind.NOT_FOUND,
                "workbook resource was not found or is not visible to the connection",
            ),
            0,
            receipt,
        )
    if category == "tooLarge":
        return (
            oversized_branch,
            _failure(
                operation, FailureKind.RESPONSE_TOO_LARGE, "Excel reported that the range exceeds its limit"
            ),
            0,
            receipt,
        )
    return "providerRejected", _failure(operation, kind, "provider rejected the request"), 0, receipt


async def _read(
    client: Client,
    call: Call,
    credential: Any,
    operation: str,
    method: str,
    target: str,
    body: bytes | None = None,
    oversized_branch: str = "tooLarge",
) -> tuple[GraphResponse | None, tuple[str, Failure | None, float, Receipt] | None]:
    try:
        response = await client.request(call, credential, method, target, body)
        return response, _read_result(client, call, operation, response, None, oversized_branch)
    except GraphTransportError as error:
        return None, _read_result(client, call, operation, None, error, oversized_branch)


async def _columns(
    client: Client,
    call: Call,
    credential: Any,
    operation: str,
    input: Any,
    oversized_branch: str = "tooLarge",
) -> tuple[list[tuple[str, str]] | None, tuple[str, Failure | None, float, Receipt] | None]:
    response, outcome = await _read(
        client,
        call,
        credential,
        operation,
        "GET",
        table_url(input.drive_id, input.workbook_id, input.table) + "/columns" + _QUERY_SELECT_COLUMNS,
        oversized_branch=oversized_branch,
    )
    if outcome is not None:
        return None, outcome
    assert response is not None
    try:
        return _decode_columns(response.body), None
    except ValueError:
        return None, (
            "invalidResponse",
            _failure(operation, FailureKind.PROTOCOL, "provider returned an invalid table columns response"),
            0,
            _receipt(client, call, response),
        )


def _query_outcome(outcome: tuple[str, Failure | None, float, Receipt], zero: Any) -> QueryAttempt[Any]:
    branch, failure, delay, receipt = outcome
    if branch == "retry":
        assert failure is not None
        return QueryAttempt.retry(failure, delay)
    return QueryAttempt.branch_result(branch, zero, receipt, failure)


def _mutation_outcome(outcome: tuple[str, Failure | None, float, Receipt], zero: Any) -> MutationAttempt[Any]:
    branch, failure, delay, receipt = outcome
    if branch == "retry":
        assert failure is not None
        return MutationAttempt.retry(failure, delay)
    return MutationAttempt.branch_result(branch, zero, receipt, failure)


class GetValuesOperation:
    """Read a bounded Excel worksheet range."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated getValues definition."""
        return GET_VALUES_DEFINITION

    async def invoke(self, call: Call, input: GetValuesInput) -> QueryAttempt[GetValuesOutput]:
        """Read raw and displayed cells from one Graph workbookRange."""
        operation = "getValues"
        try:
            address, rows, columns = _a1_address(
                input.drive_id,
                input.workbook_id,
                input.worksheet,
                input.address,
                self.client.config.max_cells,
            )
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect",
                GetValuesOutput(),
                Receipt(),
                _failure(operation, FailureKind.VALIDATION, str(error)),
            )
        try:
            credential = await self.client.resolve_credentials(call)
        except Exception:
            return QueryAttempt.branch_result(
                "defect",
                GetValuesOutput(),
                Receipt(),
                _failure(operation, FailureKind.AUTHENTICATION, "connection credentials are unavailable"),
            )
        response, outcome = await _read(
            self.client,
            call,
            credential,
            operation,
            "GET",
            range_url(input.drive_id, input.workbook_id, input.worksheet, address) + _QUERY_SELECT_TEXTS,
        )
        if outcome is not None:
            return _query_outcome(outcome, GetValuesOutput())
        assert response is not None
        receipt = _receipt(self.client, call, response)
        try:
            returned_address, returned_rows, returned_columns, values, text = _decode_range(
                response.body, True
            )
            if (returned_rows, returned_columns) != (rows, columns):
                raise ValueError("range size differs from the requested address")
        except (ValueError, LookupError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                GetValuesOutput(),
                receipt,
                _failure(operation, FailureKind.PROTOCOL, "provider returned an invalid range response"),
            )
        return QueryAttempt.branch_result(
            "read",
            GetValuesOutput(
                returned_address,
                rows,
                columns,
                tuple(tuple(row) for row in values),
                tuple(tuple(row) for row in text),
            ),
            receipt,
        )


class GetTableRowsOperation:
    """Read an Excel table as rows keyed by column name."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated getTableRows definition."""
        return GET_TABLE_ROWS_DEFINITION

    async def invoke(self, call: Call, input: GetTableRowsInput) -> QueryAttempt[GetTableRowsOutput]:
        """Read columns and data body within the configured limits."""
        operation = "getTableRows"
        try:
            _check_table(input.drive_id, input.workbook_id, input.table)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect",
                GetTableRowsOutput(),
                Receipt(),
                _failure(operation, FailureKind.VALIDATION, str(error)),
            )
        try:
            credential = await self.client.resolve_credentials(call)
        except Exception:
            return QueryAttempt.branch_result(
                "defect",
                GetTableRowsOutput(),
                Receipt(),
                _failure(operation, FailureKind.AUTHENTICATION, "connection credentials are unavailable"),
            )
        columns, outcome = await _columns(self.client, call, credential, operation, input)
        if outcome is not None:
            return _query_outcome(outcome, GetTableRowsOutput())
        assert columns is not None
        response, outcome = await _read(
            self.client,
            call,
            credential,
            operation,
            "GET",
            table_url(input.drive_id, input.workbook_id, input.table)
            + "/dataBodyRange"
            + _QUERY_SELECT_VALUES,
        )
        if outcome is not None:
            return _query_outcome(outcome, GetTableRowsOutput())
        assert response is not None
        receipt = _receipt(self.client, call, response)
        try:
            _, rows, count, values, _ = _decode_range(response.body, False)
            if rows * count > self.client.config.max_cells:
                return QueryAttempt.branch_result(
                    "tooLarge",
                    GetTableRowsOutput(),
                    receipt,
                    _failure(
                        operation,
                        FailureKind.RESPONSE_TOO_LARGE,
                        "table holds more cells than the configured cell limit",
                    ),
                )
            if count != len(columns):
                raise ValueError("table columns changed")
        except LookupError:
            return QueryAttempt.branch_result(
                "tooLarge",
                GetTableRowsOutput(),
                receipt,
                _failure(
                    operation,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "table holds more cells than the configured cell limit",
                ),
            )
        except ValueError:
            return QueryAttempt.branch_result(
                "invalidResponse",
                GetTableRowsOutput(),
                receipt,
                _failure(operation, FailureKind.PROTOCOL, "provider returned an invalid table data response"),
            )
        result_rows = tuple(
            TableRow(index, {column[0]: row[position] for position, column in enumerate(columns)})
            for index, row in enumerate(values)
        )
        return QueryAttempt.branch_result(
            "read", GetTableRowsOutput(tuple(name for name, _ in columns), result_rows), receipt
        )


class UpdateValuesOperation:
    """Write absolute literal values into one bounded range."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated updateValues definition."""
        return UPDATE_VALUES_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpdateValuesInput) -> IdempotencyKey:
        """Use the stable Call ID because repeated absolute writes are safe."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: UpdateValuesInput) -> MutationAttempt[UpdateValuesOutput]:
        """Write one range, retrying transient or unknown provider outcomes."""
        operation = "updateValues"
        try:
            address, rows, columns = _a1_address(
                input.drive_id,
                input.workbook_id,
                input.worksheet,
                input.address,
                self.client.config.max_cells,
            )
            if len(input.values) != rows or any(len(row) != columns for row in input.values):
                raise ValueError("values dimensions differ from the requested address")
            wire = [[_wire_cell(cell) for cell in row] for row in input.values]
            body = compact_json({"values": wire})
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect",
                UpdateValuesOutput(),
                Receipt(),
                _failure(operation, FailureKind.VALIDATION, str(error)),
            )
        try:
            credential = await self.client.resolve_credentials(call)
        except Exception:
            return MutationAttempt.branch_result(
                "defect",
                UpdateValuesOutput(),
                Receipt(),
                _failure(operation, FailureKind.AUTHENTICATION, "connection credentials are unavailable"),
            )
        try:
            response = await self.client.request(
                call,
                credential,
                "PATCH",
                range_url(input.drive_id, input.workbook_id, input.worksheet, address),
                body,
            )
        except GraphTransportError as error:
            if not error.is_request_built:
                return MutationAttempt.branch_result(
                    "defect",
                    UpdateValuesOutput(),
                    Receipt(),
                    _failure(operation, FailureKind.LOCAL_DEFECT, str(error)),
                )
            return MutationAttempt.retry(
                _failure(
                    operation,
                    FailureKind.TRANSPORT,
                    "provider is unavailable; the same values are written again",
                )
            )
        receipt = _receipt(self.client, call, response)
        if 200 <= response.status < 300:
            output_address = address
            try:
                returned = json.loads(response.body)
                if (
                    isinstance(returned, dict)
                    and isinstance(returned.get("address"), str)
                    and returned["address"]
                ):
                    output_address = returned["address"]
            except (ValueError, UnicodeError):
                pass  # Go keeps the validated address when the optional response body is unreadable.
            return MutationAttempt.branch_result(
                "updated", UpdateValuesOutput(output_address, rows, columns), receipt
            )
        tokens = _tokens(response.body)
        category = _error_class(response.status, tokens)
        kind = _status_kind(response.status, tokens)
        if category == "retry":
            return MutationAttempt.retry(
                _failure(operation, kind, "provider temporarily rejected the write"),
                _retry_after(response, self.client.now()),
            )
        if category == "notFound":
            return MutationAttempt.branch_result(
                "notFound",
                UpdateValuesOutput(),
                receipt,
                _failure(
                    operation,
                    FailureKind.NOT_FOUND,
                    "workbook or worksheet was not found or is not visible to the connection",
                ),
            )
        return MutationAttempt.branch_result(
            "providerRejected",
            UpdateValuesOutput(),
            receipt,
            _failure(operation, kind, "provider rejected the write"),
        )


class AppendTableRowsOperation:
    """Append missing table rows once per Dex Step execution."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated appendTableRows definition."""
        return APPEND_TABLE_ROWS_DEFINITION

    def idempotency_key(self, call_id: CallID, input: AppendTableRowsInput) -> IdempotencyKey:
        """Use Call ID to identify the Step's single-dispatch checkpoint."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: AppendTableRowsInput) -> MutationAttempt[AppendTableRowsOutput]:
        """Read keys, persist a dispatch marker, and append only missing rows."""
        operation = "appendTableRows"
        zero = AppendTableRowsOutput()
        rows_input: list[dict[str, Any]] = []
        try:
            _check_table(input.drive_id, input.workbook_id, input.table)
            _check_reference(input.key_column, "keyColumn")
            if not input.rows or len(input.rows) > self.client.config.max_cells:
                raise ValueError("rows must hold at least one row and at most the configured cell limit")
            keys = []
            seen = set()
            for row in input.rows:
                if not isinstance(row, dict) or input.key_column not in row:
                    raise ValueError("row must set the key column")
                rows_input.append(row)
                key = row[input.key_column]
                _wire_cell(key)
                identity = _key_identity(key)
                if key == "" or identity in seen:
                    raise ValueError("row key is empty or repeated")
                seen.add(identity)
                keys.append(key)
                for column_name, value in row.items():
                    _check_reference(column_name, "column")
                    _wire_cell(value)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", zero, Receipt(), _failure(operation, FailureKind.VALIDATION, str(error))
            )
        try:
            credential = await self.client.resolve_credentials(call)
        except Exception:
            return MutationAttempt.branch_result(
                "defect",
                zero,
                Receipt(),
                _failure(operation, FailureKind.AUTHENTICATION, "connection credentials are unavailable"),
            )
        columns, outcome = await _columns(self.client, call, credential, operation, input, "invalidResponse")
        if outcome is not None:
            return _mutation_outcome(outcome, zero)
        assert columns is not None
        column_by_name = dict(columns)
        if input.key_column not in column_by_name or any(
            name not in column_by_name for row in rows_input for name in row
        ):
            return MutationAttempt.branch_result(
                "columnsMismatch",
                zero,
                Receipt(),
                _failure(
                    operation,
                    FailureKind.VALIDATION,
                    "the table has no column with the key column's name or with a name a row uses",
                ),
            )
        if len(rows_input) * len(columns) > self.client.config.max_cells:
            return MutationAttempt.branch_result(
                "defect",
                zero,
                Receipt(),
                _failure(
                    operation,
                    FailureKind.VALIDATION,
                    "rows times table columns exceed the configured cell limit",
                ),
            )
        key_url = (
            table_url(input.drive_id, input.workbook_id, input.table)
            + "/columns/"
            + path_escape(column_by_name[input.key_column])
            + "/dataBodyRange"
            + _QUERY_SELECT_VALUES
        )
        response, outcome = await _read(
            self.client, call, credential, operation, "GET", key_url, oversized_branch="invalidResponse"
        )
        if outcome is not None:
            return _mutation_outcome(outcome, zero)
        assert response is not None
        receipt = _receipt(self.client, call, response)
        try:
            _, _, count, values, _ = _decode_range(response.body, False)
            if count != 1:
                raise ValueError("key column width differs")
        except (ValueError, LookupError):
            return MutationAttempt.branch_result(
                "invalidResponse",
                zero,
                receipt,
                _failure(
                    operation,
                    FailureKind.PROTOCOL,
                    "provider returned an invalid key column response before the append was sent",
                ),
            )
        stored = [row[0] for row in values]
        present, missing, indexes = [], [], []
        for index, key in enumerate(keys):
            if any(_matches_stored_key(key, item) for item in stored):
                present.append(key)
            else:
                missing.append(key)
                indexes.append(index)
        context = call.context
        try:
            has_prior = bool(
                context.get_last_heartbeat_value(_DispatchMarker)
                if context and context.has_last_heartbeat_value()
                else None
            )
        except Exception:
            has_prior = True  # An unreadable checkpoint still prevents a repeated append.
        if not missing:
            return MutationAttempt.branch_result(
                "appended", AppendTableRowsOutput((), tuple(present), has_prior), receipt
            )
        body = compact_json(
            {
                "values": [
                    [_wire_cell(rows_input[index].get(name, "")) for name, _ in columns] for index in indexes
                ]
            }
        )
        if has_prior:
            return MutationAttempt.uncertain(
                AppendTableRowsOutput((), tuple(present)),
                _failure(
                    operation,
                    FailureKind.TRANSPORT,
                    "an earlier attempt of this Step sent the append, "
                    "and the key column does not show every row yet",
                ),
                receipt,
            )
        if context is None:
            return MutationAttempt.retry(
                _failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "Dex did not record the dispatch checkpoint; nothing was sent",
                )
            )
        try:
            await context.heartbeat(_DispatchMarker(str(call.id)))
        except Exception:
            return MutationAttempt.retry(
                _failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "Dex did not record the dispatch checkpoint; nothing was sent",
                )
            )
        try:
            appended = await self.client.request(
                call,
                credential,
                "POST",
                table_url(input.drive_id, input.workbook_id, input.table) + "/rows/add",
                body,
            )
        except GraphTransportError as error:
            if not error.is_request_built:
                await context.heartbeat()
                return MutationAttempt.branch_result(
                    "defect", zero, Receipt(), _failure(operation, FailureKind.LOCAL_DEFECT, str(error))
                )
            return MutationAttempt.retry(
                _failure(
                    operation,
                    FailureKind.TRANSPORT,
                    "append outcome is unknown; the next attempt checks the key column "
                    "instead of sending again",
                ),
                10,
            )
        append_receipt = _receipt(self.client, call, appended)
        if 200 <= appended.status < 300:
            return MutationAttempt.branch_result(
                "appended", AppendTableRowsOutput(tuple(missing), tuple(present)), append_receipt
            )
        tokens = _tokens(appended.body)
        kind = _status_kind(appended.status, tokens)
        if appended.status == 408 or not (400 <= appended.status < 500 or appended.status in (501, 503)):
            return MutationAttempt.retry(
                _failure(
                    operation,
                    kind,
                    "append outcome is unknown; the next attempt checks the key column "
                    "instead of sending again",
                ),
                10,
            )
        try:
            await context.heartbeat()
        except Exception:
            pass  # A failed clear leaves the marker, so the next attempt does not send again.
        category = _error_class(appended.status, tokens)
        if category == "retry":
            return MutationAttempt.retry(
                _failure(operation, kind, "provider temporarily refused the append; nothing was written"),
                _retry_after(appended, self.client.now()),
            )
        if category == "notFound":
            return MutationAttempt.branch_result(
                "notFound",
                zero,
                append_receipt,
                _failure(
                    operation,
                    FailureKind.NOT_FOUND,
                    "workbook or table was not found or is not visible to the connection",
                ),
            )
        return MutationAttempt.branch_result(
            "providerRejected",
            zero,
            append_receipt,
            _failure(operation, kind, "provider rejected the append; nothing was written"),
        )
