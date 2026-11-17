# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Google Sheets operation invocations and Go-compatible outcome classification."""

import math
import re
from dataclasses import dataclass
from datetime import datetime
from decimal import Decimal

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
from dex_connectors.sdk.singledispatch import (
    CheckpointError,
    DecisionKind,
    DispatchNotAllowedError,
    Fence,
    InsufficientAttemptTimeError,
    State,
)

from ._generated.definitions import FIND_ROW_DEFINITION, GET_VALUES_DEFINITION, UPSERT_ROW_DEFINITION
from .client import Client, Credentials, ProviderResponse, RequestError, escape_path, failure
from .json_values import strict_json_loads
from .models import (
    FindRowInput,
    FindRowOutput,
    GetValuesInput,
    GetValuesOutput,
    UpsertRowInput,
    UpsertRowOutput,
)


def _status_kind(status: int) -> FailureKind:
    return {
        401: FailureKind.AUTHENTICATION,
        403: FailureKind.AUTHORIZATION,
        404: FailureKind.NOT_FOUND,
        409: FailureKind.CONFLICT,
        429: FailureKind.RATE_LIMIT,
    }.get(status, FailureKind.AVAILABILITY if status >= 500 else FailureKind.PROVIDER_REJECTION)


def _retry_delay(response: ProviderResponse) -> tuple[float, bool]:
    value = response.headers.get("Retry-After", "")
    if not value:
        return 0, True
    if not re.fullmatch(r"[+-]?[0-9]+", value):
        return 0, False
    seconds = int(value)
    if seconds < 0 or seconds > (2**63 - 1) // 1_000_000_000:
        return 0, False
    return float(seconds), True


def _convert_cell(value: object) -> str:
    if value is None:
        return "<nil>"
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, float):
        if value == 0:
            return "-0" if math.copysign(1, value) < 0 else "0"
        decimal = Decimal(repr(value)).normalize()
        exponent = decimal.adjusted()
        if exponent < -4 or exponent >= 6:
            digits = "".join(str(digit) for digit in decimal.as_tuple().digits)
            coefficient = digits[0] + ("." + digits[1:] if len(digits) > 1 else "")
            sign = "-" if decimal.is_signed() else ""
            return f"{sign}{coefficient}e{exponent:+03d}"
        return format(decimal, "f")
    if isinstance(value, int):
        return str(value)
    if isinstance(value, list):
        return "[" + " ".join(_convert_cell(item) for item in value) + "]"
    if isinstance(value, dict):
        return "map[" + " ".join(f"{key}:{_convert_cell(value[key])}" for key in sorted(value)) + "]"
    return str(value)



def _decode_values(
    response: ProviderResponse, max_rows: int
) -> tuple[GetValuesOutput | None, FailureKind | None]:
    if response.too_large:
        return None, FailureKind.RESPONSE_TOO_LARGE
    try:
        document = strict_json_loads(response.body)
        if not isinstance(document, dict):
            raise ValueError("response must be object")
        value_range = document.get("range", "")
        dimension = document.get("majorDimension", "")
        rows = document.get("values", [])
        if value_range is None:
            value_range = ""
        if dimension is None:
            dimension = ""
        if rows is None:
            rows = []
        if not isinstance(value_range, str) or not isinstance(dimension, str) or not isinstance(rows, list):
            raise ValueError("invalid values response")
        if len(rows) > max_rows:
            return None, FailureKind.RESPONSE_TOO_LARGE
        if any(row is not None and not isinstance(row, list) for row in rows):
            raise ValueError("invalid row")
        return GetValuesOutput(
            value_range, dimension, tuple(tuple(_convert_cell(cell) for cell in (row or [])) for row in rows)
        ), None
    except (ValueError, TypeError, UnicodeDecodeError):
        return None, FailureKind.PROTOCOL


def _find_row(
    rows: tuple[tuple[str, ...], ...], key_column: str, key_value: str
) -> tuple[str, FindRowOutput, Failure | None]:
    empty = FindRowOutput()
    if not rows:
        return "defect", empty, failure("findRow", FailureKind.VALIDATION, "sheet must contain a header row")
    headers = rows[0]
    columns = [index for index, name in enumerate(headers) if name == key_column]
    if len(columns) > 1:
        return "conflict", empty, failure("findRow", FailureKind.CONFLICT, "key column header is duplicated")
    if not columns:
        return "defect", empty, failure("findRow", FailureKind.VALIDATION, "key column is missing")
    column = columns[0]
    matches = [
        (index + 1, row)
        for index, row in enumerate(rows)
        if index > 0 and len(row) > column and row[column] == key_value
    ]
    if not matches:
        return "notFound", empty, None
    if len(matches) > 1:
        return (
            "conflict",
            FindRowOutput(conflicting_rows=tuple(number for number, _ in matches)),
            failure("findRow", FailureKind.CONFLICT, "multiple rows contain the stable key"),
        )
    number, row = matches[0]
    return (
        "found",
        FindRowOutput(
            row_number=number,
            values={header: row[index] for index, header in enumerate(headers) if index < len(row)},
        ),
        None,
    )


def _required_find_input(input: FindRowInput) -> bool:
    return all(
        (
            input.spreadsheet_id.strip(),
            input.sheet_name.strip(),
            input.key_column.strip(),
            input.key_value.strip(),
        )
    )


def _sheet_range(sheet_name: str) -> str:
    return "'" + sheet_name.replace("'", "''") + "'"


def _read_path(sheet_name: str) -> str:
    return "/values/" + escape_path(_sheet_range(sheet_name))


class GetValuesOperation:
    """Read a bounded A1 range."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query contract."""
        return GET_VALUES_DEFINITION

    async def invoke(self, call: Call, input: GetValuesInput) -> QueryAttempt[GetValuesOutput]:
        """Read and classify a Google Sheets range."""
        operation, empty = "getValues", GetValuesOutput()
        if not input.spreadsheet_id.strip() or not input.range.strip():
            return QueryAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                failure(operation, FailureKind.VALIDATION, "spreadsheet ID and range are required"),
            )
        credential, credential_failure, _ = await self.client.resolve(call, operation)
        if credential_failure is not None or credential is None:
            return QueryAttempt.branch_result("defect", empty, Receipt(), credential_failure)
        try:
            response, _ = await self.client.request(
                call,
                credential,
                "GET",
                input.spreadsheet_id,
                "/values/" + escape_path(input.range),
                {"majorDimension": "ROWS", "valueRenderOption": "FORMATTED_VALUE"},
            )
        except RequestError as error:
            if error.kind == FailureKind.LOCAL_DEFECT:
                return QueryAttempt.branch_result(
                    "defect", empty, Receipt(), failure(operation, error.kind, str(error))
                )
            return QueryAttempt.retry(failure(operation, FailureKind.AVAILABILITY, "provider is unavailable"))
        receipt = self.client.receipt(call, response)
        if response.too_large:
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                failure(
                    operation, FailureKind.RESPONSE_TOO_LARGE, "provider response exceeds configured limit"
                ),
            )
        if response.status == 404:
            return QueryAttempt.branch_result(
                "notFound",
                empty,
                receipt,
                failure(operation, FailureKind.NOT_FOUND, "spreadsheet or range was not found"),
            )
        if response.status == 429 or response.status >= 500:
            delay, valid = _retry_delay(response)
            return QueryAttempt.retry(
                failure(
                    operation,
                    _status_kind(response.status) if valid else FailureKind.PROTOCOL,
                    "provider temporarily rejected the query"
                    if valid
                    else "provider returned an invalid Retry-After header",
                ),
                delay if valid else 0,
            )
        if not 200 <= response.status < 300:
            return QueryAttempt.branch_result(
                "providerRejected",
                empty,
                receipt,
                failure(operation, _status_kind(response.status), "provider rejected the query"),
            )
        output, decode_failure = _decode_values(response, self.client.config.max_rows)
        if decode_failure is not None or output is None:
            message = (
                "provider response exceeds configured row limit"
                if decode_failure == FailureKind.RESPONSE_TOO_LARGE
                else "provider response is invalid"
            )
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                failure(operation, decode_failure or FailureKind.PROTOCOL, message),
            )
        return QueryAttempt.branch_result("read", output, receipt)


class FindRowOperation:
    """Find exactly one row by stable key."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query contract."""
        return FIND_ROW_DEFINITION

    async def invoke(self, call: Call, input: FindRowInput) -> QueryAttempt[FindRowOutput]:
        """Read a tab and classify its key lookup."""
        operation, empty = "findRow", FindRowOutput()
        if not _required_find_input(input):
            return QueryAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                failure(
                    operation,
                    FailureKind.VALIDATION,
                    "spreadsheet ID, sheet name, key column, and key value are required",
                ),
            )
        credential, credential_failure, _ = await self.client.resolve(call, operation)
        if credential_failure is not None or credential is None:
            return QueryAttempt.branch_result("defect", empty, Receipt(), credential_failure)
        try:
            response, _ = await self.client.request(
                call,
                credential,
                "GET",
                input.spreadsheet_id,
                _read_path(input.sheet_name),
                {"majorDimension": "ROWS", "valueRenderOption": "FORMATTED_VALUE"},
            )
        except RequestError as error:
            if error.kind == FailureKind.LOCAL_DEFECT:
                return QueryAttempt.branch_result(
                    "defect", empty, Receipt(), failure(operation, error.kind, str(error))
                )
            return QueryAttempt.retry(failure(operation, FailureKind.AVAILABILITY, "provider is unavailable"))
        receipt = self.client.receipt(call, response)
        if response.too_large:
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                failure(
                    operation, FailureKind.RESPONSE_TOO_LARGE, "provider response exceeds configured limit"
                ),
            )
        if response.status == 404:
            return QueryAttempt.branch_result(
                "notFound",
                empty,
                receipt,
                failure(operation, FailureKind.NOT_FOUND, "spreadsheet or sheet was not found"),
            )
        if response.status == 429 or response.status >= 500:
            delay, valid = _retry_delay(response)
            return QueryAttempt.retry(
                failure(
                    operation,
                    _status_kind(response.status) if valid else FailureKind.PROTOCOL,
                    "provider temporarily rejected the query"
                    if valid
                    else "provider returned an invalid Retry-After header",
                ),
                delay if valid else 0,
            )
        if not 200 <= response.status < 300:
            return QueryAttempt.branch_result(
                "providerRejected",
                empty,
                receipt,
                failure(operation, _status_kind(response.status), "provider rejected the query"),
            )
        output, decode_failure = _decode_values(response, self.client.config.max_rows)
        if decode_failure is not None or output is None:
            message = (
                "provider response exceeds configured row limit"
                if decode_failure == FailureKind.RESPONSE_TOO_LARGE
                else "provider response is invalid"
            )
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                failure(operation, decode_failure or FailureKind.PROTOCOL, message),
            )
        branch, found, find_failure = _find_row(output.values, input.key_column, input.key_value)
        return QueryAttempt.branch_result(branch, found, receipt, find_failure)


def _build_row(
    rows: tuple[tuple[str, ...], ...], input: UpsertRowInput
) -> tuple[tuple[str, ...] | None, str]:
    if not rows:
        return None, "sheet must contain a header row"
    values = dict(input.values or {})
    if input.key_column in values and values[input.key_column] != input.key_value:
        return None, "key column value conflicts with stable key"
    values[input.key_column] = input.key_value
    headers = rows[0]
    if any(not header for header in headers) or len(set(headers)) != len(headers):
        return None, "headers must be non-empty and unique"
    unknown = sorted(set(values) - set(headers))
    if unknown:
        return None, "values contain unknown columns: " + ", ".join(unknown)
    return tuple(values.get(header, "") for header in headers), ""


def _column_name(number: int) -> str:
    result = ""
    while number:
        number -= 1
        result = chr(65 + number % 26) + result
        number //= 26
    return result


def _upsert_output(action: str, row_number: int, sheet_name: str, column_count: int) -> UpsertRowOutput:
    return UpsertRowOutput(
        action,
        row_number,
        f"{_sheet_range(sheet_name)}!A{row_number}:{_column_name(column_count)}{row_number}",
    )


def _first_row(updated_range: str) -> int:
    match = re.search(r"[0-9]+", updated_range.rsplit("!", 1)[-1]) if "!" in updated_range else None
    return int(match.group()) if match and int(match.group()) > 0 else 0


# Only these statuses prove Google refused a write; every other status is an unknown outcome.
_DEFINITE_REFUSAL_STATUSES = frozenset({400, 401, 403, 404, 411, 413, 414, 415, 422})


@dataclass(frozen=True)
class _AppendSend:
    """Send-cap facts of this attempt's possible append."""

    # False when this append is the last one the cap allows.
    can_resend_unknown_outcome: bool
    has_possible_earlier_send: bool


def _has_append_acceptance(response: ProviderResponse) -> bool:
    """Report a response body that names the appended range."""
    if response.too_large:
        return False
    try:
        body = strict_json_loads(response.body)
    except (ValueError, UnicodeDecodeError):
        return False
    updates = body.get("updates") if isinstance(body, dict) else None
    updated = updates.get("updatedRange") if isinstance(updates, dict) else None
    return isinstance(updated, str) and bool(updated)


class _AppendWriteAttempt:
    def __init__(
        self,
        client: Client,
        call: Call,
        credential: Credentials,
        input: UpsertRowInput,
        target_range: str,
        payload: dict[str, object],
    ) -> None:
        self.client = client
        self.call = call
        self.credential = credential
        self.input = input
        self.target_range = target_range
        self.payload = payload
        self.response = ProviderResponse()
        self.request_error: RequestError | None = None

    async def send(self, _deadline: datetime) -> None:
        try:
            self.response, _ = await self.client.request(
                self.call,
                self.credential,
                "POST",
                self.input.spreadsheet_id,
                "/values/" + escape_path(self.target_range) + ":append",
                {"insertDataOption": "INSERT_ROWS", "valueInputOption": "RAW"},
                self.payload,
                refresh=False,
                write=True,
            )
        except RequestError as error:
            self.request_error = error


class UpsertRowOperation:
    """Read by stable key, replace safely, or fence one append."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated Mutation contract."""
        return UPSERT_ROW_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpsertRowInput) -> IdempotencyKey:
        """Use the stable Dex call ID as the Google correlation header."""
        return IdempotencyKey(call_id)

    def _branch(
        self, branch: str, kind: FailureKind, message: str, receipt: Receipt | None = None
    ) -> MutationAttempt[UpsertRowOutput]:
        return MutationAttempt.branch_result(
            branch, UpsertRowOutput(), receipt or Receipt(), failure("upsertRow", kind, message)
        )

    def _uncertain(
        self, call: Call, message: str, response: ProviderResponse | None = None
    ) -> MutationAttempt[UpsertRowOutput]:
        return MutationAttempt.uncertain(
            UpsertRowOutput(),
            failure("upsertRow", FailureKind.TRANSPORT, message),
            self.client.receipt(call, response),
        )

    async def _retry_before_write(
        self, fence: Fence, decision: DecisionKind, kind: FailureKind, message: str, delay: float = 0
    ) -> MutationAttempt[UpsertRowOutput]:
        if decision == DecisionKind.SEND_ALLOWED:
            try:
                await fence.record_not_sent()
            except Exception:
                kind, message = (
                    FailureKind.AVAILABILITY,
                    "pre-send checkpoint failed; retry remains fail-closed",
                )
        return MutationAttempt.retry(failure("upsertRow", kind, message), delay)

    async def invoke(self, call: Call, input: UpsertRowInput) -> MutationAttempt[UpsertRowOutput]:
        """Preserve Go's prewrite, append, readback, and uncertainty decisions.

        The key lookup reads back an earlier possible append; an unconfirmed
        append is resent while possible sends stay below the send cap, and
        uncertain is selected only at the cap.
        """
        find_input = FindRowInput(input.spreadsheet_id, input.sheet_name, input.key_column, input.key_value)
        if not _required_find_input(find_input) or not input.values:
            message = (
                "values are required"
                if _required_find_input(find_input)
                else "spreadsheet ID, sheet name, key column, and key value are required"
            )
            return self._branch("defect", FailureKind.VALIDATION, message)
        try:
            fence = Fence(
                call, UPSERT_ROW_DEFINITION.step_defaults.execute_method_timeout, max_sends=call.max_sends
            )
        except Exception:
            return self._branch(
                "defect", FailureKind.LOCAL_DEFECT, "single-dispatch fence could not be constructed"
            )
        decision = await fence.decide()
        if decision.kind == DecisionKind.FAIL_CLOSED:
            return MutationAttempt.retry(
                failure(
                    "upsertRow",
                    FailureKind.AVAILABILITY,
                    "prior dispatch checkpoint is unreadable; no row was written in this attempt",
                )
            )
        # An earlier append may have taken effect, so a terminal non-uncertain branch could hide it.
        has_possible_earlier_send = decision.prior_send_upper_bound > 0
        credential, credential_failure, terminal = await self.client.resolve(call, "upsertRow")
        if credential_failure is not None or credential is None:
            if terminal:
                if not has_possible_earlier_send and decision.kind == DecisionKind.SEND_ALLOWED:
                    try:
                        await fence.record_not_sent()
                    except Exception:
                        return MutationAttempt.retry(
                            failure("upsertRow", FailureKind.AVAILABILITY, "credential checkpoint failed")
                        )
                    return MutationAttempt.branch_result(
                        "defect", UpsertRowOutput(), Receipt(), credential_failure
                    )
                return MutationAttempt.uncertain(
                    UpsertRowOutput(),
                    failure(
                        "upsertRow",
                        FailureKind.AUTHENTICATION,
                        "prior append cannot be checked; reconnect the connection",
                    ),
                    self.client.receipt(
                        call, object_id=decision.marker.provider_id if decision.marker else ""
                    ),
                )
            return await self._retry_before_write(
                fence,
                decision.kind,
                FailureKind.AVAILABILITY,
                "credential resolution is temporarily unavailable",
            )
        try:
            lookup, credential = await self.client.request(
                call,
                credential,
                "GET",
                input.spreadsheet_id,
                _read_path(input.sheet_name),
                {"majorDimension": "ROWS", "valueRenderOption": "FORMATTED_VALUE"},
            )
        except RequestError as error:
            if error.kind == FailureKind.LOCAL_DEFECT:
                if has_possible_earlier_send:
                    return self._uncertain(call, f"prior append cannot be checked: {error}")
                return self._branch("defect", error.kind, str(error))
            return await self._retry_before_write(
                fence, decision.kind, FailureKind.AVAILABILITY, "sheet lookup is unavailable"
            )
        receipt = self.client.receipt(call, lookup)
        if lookup.too_large:
            if has_possible_earlier_send:
                return self._uncertain(call, "read-back response is oversized", lookup)
            return self._branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "provider response exceeds configured limit",
                receipt,
            )
        if lookup.status == 429 or lookup.status >= 500:
            delay, valid = _retry_delay(lookup)
            return await self._retry_before_write(
                fence,
                decision.kind,
                _status_kind(lookup.status) if valid else FailureKind.PROTOCOL,
                "sheet lookup is temporarily unavailable"
                if valid
                else "provider returned an invalid Retry-After header",
                delay if valid else 0,
            )
        if not 200 <= lookup.status < 300:
            if has_possible_earlier_send:
                return self._uncertain(call, "read-back was rejected", lookup)
            return self._branch(
                "providerRejected",
                _status_kind(lookup.status),
                "provider rejected the pre-write query",
                receipt,
            )
        decoded, decode_failure = _decode_values(lookup, self.client.config.max_rows)
        if decode_failure is not None or decoded is None:
            if has_possible_earlier_send:
                return self._uncertain(call, "read-back response is invalid", lookup)
            message = (
                "provider response exceeds configured row limit"
                if decode_failure == FailureKind.RESPONSE_TOO_LARGE
                else "provider response is invalid"
            )
            return self._branch("invalidResponse", decode_failure or FailureKind.PROTOCOL, message, receipt)
        branch, found, find_failure = _find_row(decoded.values, input.key_column, input.key_value)
        if branch == "conflict":
            return MutationAttempt.branch_result("conflict", UpsertRowOutput(), receipt, find_failure)
        if branch not in ("found", "notFound"):
            if has_possible_earlier_send:
                return self._uncertain(call, "prior append cannot be checked against the row key", lookup)
            return MutationAttempt.branch_result(
                "defect",
                UpsertRowOutput(),
                receipt,
                failure(
                    "upsertRow",
                    FailureKind.VALIDATION,
                    find_failure.message if find_failure else "invalid row",
                ),
            )
        row, row_error = _build_row(decoded.values, input)
        if row is None:
            if has_possible_earlier_send:
                return self._uncertain(call, "prior append cannot be checked against the row schema", lookup)
            return self._branch("defect", FailureKind.VALIDATION, row_error, receipt)
        if (
            branch == "found"
            and found.values.get(input.key_column) == input.key_value
            and all(found.values.get(key, "") == value for key, value in input.values.items())
        ):
            action = (
                "inserted"
                if decision.marker and decision.marker.state in (State.CLAIMED, State.CAN_RESEND)
                else "updated"
            )
            return MutationAttempt.branch_result(
                "upserted", _upsert_output(action, found.row_number, input.sheet_name, len(row)), receipt
            )
        if decision.kind == DecisionKind.READ_BACK_REQUIRED and (
            decision.marker and decision.marker.state == State.CLAIMED or branch == "notFound"
        ):
            return self._uncertain(call, "prior append cannot be confirmed by the row key", lookup)
        if branch == "found":
            return await self._replace(
                call,
                credential,
                input,
                row,
                found.row_number,
                fence,
                decision.kind,
                has_possible_earlier_send,
            )
        if (
            decision.kind == DecisionKind.READ_BACK_THEN_RESEND_ALLOWED
            and decision.marker is not None
            and decision.marker.provider_id
        ):
            # Google accepted an earlier append, so a missing row is read lag and never resent.
            return self._uncertain(call, "accepted append is not yet visible by the row key", lookup)
        append_send = _AppendSend(
            can_resend_unknown_outcome=decision.prior_send_upper_bound + 1 < call.max_sends,
            has_possible_earlier_send=has_possible_earlier_send,
        )
        return await self._append(call, credential, input, row, fence, append_send)

    async def _replace(
        self,
        call: Call,
        credential: Credentials,
        input: UpsertRowInput,
        row: tuple[str, ...],
        number: int,
        fence: Fence,
        decision: DecisionKind,
        has_possible_earlier_send: bool,
    ) -> MutationAttempt[UpsertRowOutput]:
        target_range = f"{_sheet_range(input.sheet_name)}!A{number}"
        payload: dict[str, object] = {"range": target_range, "majorDimension": "ROWS", "values": [list(row)]}
        response = ProviderResponse()
        terminal = True
        for attempt in range(2):
            try:
                response, _ = await self.client.request(
                    call,
                    credential,
                    "PUT",
                    input.spreadsheet_id,
                    "/values/" + escape_path(target_range),
                    {"valueInputOption": "RAW"},
                    payload,
                    refresh=False,
                    write=True,
                )
            except RequestError as error:
                if error.kind == FailureKind.LOCAL_DEFECT:
                    if has_possible_earlier_send:
                        return self._uncertain(call, f"prior append cannot be checked: {error}")
                    return self._branch("defect", error.kind, str(error))
                return await self._retry_before_write(
                    fence, decision, FailureKind.TRANSPORT, "row replacement response is unavailable"
                )
            if response.status != 401 or attempt > 0:
                break
            replacement, terminal = await self.client.refresh_after_rejection(call, credential)
            if replacement is None:
                break
            credential = replacement
        # An absolute replacement is repeatable, so every outcome except a definite refusal retries.
        if not 200 <= response.status < 300 and response.status not in _DEFINITE_REFUSAL_STATUSES:
            delay, valid = _retry_delay(response)
            return await self._retry_before_write(
                fence,
                decision,
                _status_kind(response.status) if valid else FailureKind.PROTOCOL,
                "row replacement is temporarily unavailable"
                if valid
                else "provider returned an invalid Retry-After header",
                delay if valid else 0,
            )
        receipt = self.client.receipt(call, response, f"{input.spreadsheet_id}#{input.sheet_name}!{number}")
        if response.status == 401:
            message = (
                "provider rejected the write credential; reconnect the connection"
                if terminal
                else "provider rejected the write credential; credential refresh is temporarily unavailable"
            )
            return self._reject_or_select_uncertain(
                has_possible_earlier_send, FailureKind.AUTHENTICATION, message, receipt
            )
        if not 200 <= response.status < 300:
            return self._reject_or_select_uncertain(
                has_possible_earlier_send,
                _status_kind(response.status),
                "provider rejected the row replacement",
                receipt,
            )
        updated = _upsert_output("updated", number, input.sheet_name, len(row)).updated_range
        if not response.too_large:
            try:
                body = strict_json_loads(response.body)
                if (
                    isinstance(body, dict)
                    and isinstance(body.get("updatedRange"), str)
                    and body["updatedRange"]
                ):
                    updated = body["updatedRange"]
            except (ValueError, UnicodeDecodeError):
                pass  # A successful absolute replacement remains accepted without a usable body.
        return MutationAttempt.branch_result("upserted", UpsertRowOutput("updated", number, updated), receipt)

    async def _append(
        self,
        call: Call,
        credential: Credentials,
        input: UpsertRowInput,
        row: tuple[str, ...],
        fence: Fence,
        append_send: _AppendSend,
    ) -> MutationAttempt[UpsertRowOutput]:
        target_range = _sheet_range(input.sheet_name)
        payload: dict[str, object] = {"range": target_range, "majorDimension": "ROWS", "values": [list(row)]}
        attempt = _AppendWriteAttempt(self.client, call, credential, input, target_range, payload)

        try:
            await fence.dispatch(attempt.send)
        except (CheckpointError, InsufficientAttemptTimeError):
            return MutationAttempt.retry(
                failure(
                    "upsertRow", FailureKind.AVAILABILITY, "append was not sent before the attempt deadline"
                )
            )
        except DispatchNotAllowedError:
            return self._uncertain(call, "single-dispatch permission is unavailable", attempt.response)
        except TimeoutError:
            return await self._reconcile(call, credential, input, row, append_send)
        response = attempt.response
        request_error = attempt.request_error
        if request_error and request_error.kind == FailureKind.LOCAL_DEFECT:
            if append_send.has_possible_earlier_send:
                return self._uncertain(call, f"prior append cannot be checked: {request_error}", response)
            return self._branch("defect", request_error.kind, str(request_error))
        is_append_accepted = request_error is None and _has_append_acceptance(response)
        if response.status == 401 and not is_append_accepted:
            replacement, terminal = await self.client.refresh_after_rejection(call, credential)
            if replacement is not None:
                return await self._retry_rejected_append(fence, response)
            message = (
                "provider rejected the write credential; reconnect the connection"
                if terminal
                else "provider rejected the write credential; credential refresh is temporarily unavailable"
            )
            return await self._reject_append_or_select_uncertain(
                fence, append_send, FailureKind.AUTHENTICATION, message, self.client.receipt(call, response)
            )
        if response.status in (429, 503):
            return await self._retry_rejected_append(fence, response)
        if response.status == 0:
            if request_error and request_error.not_sent:
                return await self._retry_rejected_append(fence, response)
            return await self._reconcile(call, credential, input, row, append_send)
        receipt = self.client.receipt(call, response)
        if response.status in _DEFINITE_REFUSAL_STATUSES and not is_append_accepted:
            return await self._reject_append_or_select_uncertain(
                fence, append_send, _status_kind(response.status), "provider rejected the append", receipt
            )
        if not 200 <= response.status < 300:
            return await self._reconcile(call, credential, input, row, append_send)
        if not response.too_large:
            try:
                body = strict_json_loads(response.body)
                updates = body.get("updates") if isinstance(body, dict) else None
                updated = updates.get("updatedRange") if isinstance(updates, dict) else None
                if isinstance(updated, str) and updated and _first_row(updated):
                    try:
                        await fence.record_provider_id(updated)
                    except Exception:
                        pass  # Acceptance is proven; the locator checkpoint is best effort.
                    return MutationAttempt.branch_result(
                        "upserted",
                        UpsertRowOutput("inserted", _first_row(updated), updated),
                        self.client.receipt(call, response, updated),
                    )
            except (ValueError, UnicodeDecodeError):
                pass  # Readback checks an accepted append when its response cannot locate the row.
        return await self._reconcile(call, credential, input, row, append_send)

    async def _retry_rejected_append(
        self, fence: Fence, response: ProviderResponse
    ) -> MutationAttempt[UpsertRowOutput]:
        try:
            await fence.record_can_resend()
        except Exception:
            return MutationAttempt.retry(
                failure("upsertRow", FailureKind.AVAILABILITY, "append retry checkpoint failed")
            )
        delay, valid = _retry_delay(response)
        kind = FailureKind.TRANSPORT if response.status == 0 else _status_kind(response.status)
        return MutationAttempt.retry(
            failure(
                "upsertRow",
                kind if valid else FailureKind.PROTOCOL,
                "append was not processed" if valid else "provider returned an invalid Retry-After header",
            ),
            delay if valid else 0,
        )

    def _resend_or_select_uncertain(
        self, call: Call, append_send: _AppendSend, response: ProviderResponse, message: str
    ) -> MutationAttempt[UpsertRowOutput]:
        """Retry an unconfirmed append while the send cap allows another send."""
        if append_send.can_resend_unknown_outcome:
            # The claim stays the last marker, so the next attempt counts this append and reads back first.
            return MutationAttempt.retry(failure("upsertRow", FailureKind.TRANSPORT, message))
        return self._uncertain(call, message + "; the send cap is reached", response)

    async def _reject_append_or_select_uncertain(
        self,
        fence: Fence,
        append_send: _AppendSend,
        kind: FailureKind,
        message: str,
        receipt: Receipt,
    ) -> MutationAttempt[UpsertRowOutput]:
        """Select providerRejected only when no earlier append may have taken effect."""
        if not append_send.has_possible_earlier_send:
            try:
                await fence.record_can_resend()
            except Exception:
                pass  # The refusal ends the Step, so a lost checkpoint cannot cause a resend.
        return self._reject_or_select_uncertain(append_send.has_possible_earlier_send, kind, message, receipt)

    def _reject_or_select_uncertain(
        self, has_possible_earlier_send: bool, kind: FailureKind, message: str, receipt: Receipt
    ) -> MutationAttempt[UpsertRowOutput]:
        """Select uncertain for a refusal after an earlier possible append."""
        if has_possible_earlier_send:
            return MutationAttempt.uncertain(
                UpsertRowOutput(),
                failure("upsertRow", kind, message + " after an earlier unconfirmed append"),
                receipt,
            )
        return self._branch("providerRejected", kind, message, receipt)

    async def _reconcile(
        self,
        call: Call,
        credential: Credentials,
        input: UpsertRowInput,
        row: tuple[str, ...],
        append_send: _AppendSend,
    ) -> MutationAttempt[UpsertRowOutput]:
        """Read back an append with an unknown outcome and resend it below the send cap."""
        try:
            response, _ = await self.client.request(
                call,
                credential,
                "GET",
                input.spreadsheet_id,
                _read_path(input.sheet_name),
                {"majorDimension": "ROWS", "valueRenderOption": "FORMATTED_VALUE"},
            )
        except RequestError:
            return MutationAttempt.retry(
                failure("upsertRow", FailureKind.AVAILABILITY, "append read-back is unavailable")
            )
        if response.status == 429 or response.status >= 500:
            delay, valid = _retry_delay(response)
            return MutationAttempt.retry(
                failure(
                    "upsertRow",
                    _status_kind(response.status) if valid else FailureKind.PROTOCOL,
                    "append read-back is temporarily unavailable"
                    if valid
                    else "read-back returned an invalid Retry-After header",
                ),
                delay if valid else 0,
            )
        if not 200 <= response.status < 300:
            return self._resend_or_select_uncertain(
                call, append_send, response, "append read-back was rejected"
            )
        decoded, decode_failure = _decode_values(response, self.client.config.max_rows)
        if decode_failure is not None or decoded is None:
            return self._resend_or_select_uncertain(
                call, append_send, response, "append read-back is invalid"
            )
        branch, found, find_failure = _find_row(decoded.values, input.key_column, input.key_value)
        if branch == "conflict":
            return MutationAttempt.branch_result(
                "conflict", UpsertRowOutput(), self.client.receipt(call, response), find_failure
            )
        if (
            branch != "found"
            or found.values.get(input.key_column) != input.key_value
            or any(found.values.get(key, "") != value for key, value in (input.values or {}).items())
        ):
            return self._resend_or_select_uncertain(
                call, append_send, response, "append cannot be confirmed by the row key"
            )
        return MutationAttempt.branch_result(
            "upserted",
            _upsert_output("inserted", found.row_number, input.sheet_name, len(row)),
            self.client.receipt(call, response),
        )
