# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Snowflake SQL API operation invocations and Go-compatible outcomes."""

import base64
import json
import math
import re
from datetime import UTC, datetime, timedelta
from decimal import Decimal
from typing import TypeVar
from urllib.parse import urlencode

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
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    parse_retry_after,
    read_bounded_body,
)

from ._generated import definitions as definitions
from .client import Client, CredentialError
from .models import (
    CancelStatementInput,
    Column,
    GetStatementResultInput,
    StatementCancellation,
    StatementResult,
    StatementStats,
    StatementSubmission,
    SubmitStatementInput,
)

_HANDLE = re.compile(r"^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$")
_CODE = re.compile(r"^[0-9]{6}$")
_SQLSTATE = re.compile(r"^[0-9A-Z]{5}$")
_DECIMAL = re.compile(r"^-?[0-9]+(?:\.[0-9]+)?$")
_DECFLOAT = re.compile(r"^-?[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$")
_JSON_NUMBER = re.compile(r"^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$")
_EPOCH = re.compile(r"^(-?)([0-9]{1,12})(?:\.([0-9]{1,9}))?$")
Output = TypeVar("Output")


class DuplicateColumnError(ValueError):
    """A statement returned column names that cannot share one row map."""


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "snowflake", operation, message)


def _status(document: object) -> dict[str, object]:
    if not isinstance(document, dict):
        return {}
    status = dict(document)
    for key, pattern in (("code", _CODE), ("sqlState", _SQLSTATE), ("statementHandle", _HANDLE)):
        if not isinstance(status.get(key), str) or not pattern.fullmatch(status[key]):
            status[key] = ""
    return status


def _created_at(status: dict[str, object]) -> datetime | None:
    milliseconds = status.get("createdOn")
    if type(milliseconds) is not int or milliseconds <= 0:
        return None
    return datetime.fromtimestamp(milliseconds / 1000, UTC)


def _receipt(client: Client, call: Call, handle: str, status: dict[str, object]) -> Receipt:
    metadata: dict[str, str] = {}
    if status.get("code"):
        metadata["code"] = str(status["code"])
    if status.get("sqlState"):
        metadata["sqlState"] = str(status["sqlState"])
    return Receipt(
        call_id=call.id,
        idempotency_key=call.idempotency_key,
        provider="snowflake",
        observed_at=client.now().astimezone(UTC),
        provider_object_id=handle if _HANDLE.fullmatch(handle) else "",
        metadata=metadata,
    )


def _rejection(operation: str, code: int, document: object) -> Failure:
    status = _status(document)
    snowflake_code = str(status.get("code") or "")
    sql_state = str(status.get("sqlState") or "")
    kind = FailureKind.PROVIDER_REJECTION
    if code == 401:
        kind = FailureKind.AUTHENTICATION
    elif code == 403:
        kind = FailureKind.AUTHORIZATION
    elif code == 404:
        kind = FailureKind.NOT_FOUND
    elif code == 400:
        kind = FailureKind.VALIDATION
    elif code == 408:
        kind = FailureKind.AVAILABILITY
    elif code == 422:
        if snowflake_code == "000709" or sql_state == "02000":
            kind = FailureKind.NOT_FOUND
        elif snowflake_code == "000604" or sql_state == "57014":
            kind = FailureKind.AVAILABILITY
        elif sql_state.startswith("28"):
            kind = FailureKind.AUTHENTICATION
        elif sql_state == "42501":
            kind = FailureKind.AUTHORIZATION
        elif sql_state.startswith("23"):
            kind = FailureKind.CONFLICT
        elif sql_state[:2] in ("22", "42", "07"):
            kind = FailureKind.VALIDATION
    elif 300 <= code < 400:
        kind = FailureKind.PROTOCOL
    message = f"Snowflake returned HTTP {code}"
    if snowflake_code:
        message += " with code " + snowflake_code
    if sql_state:
        message += " and SQLSTATE " + sql_state
    if code == 408 or snowflake_code == "000604":
        message += "; the statement exceeded statementTimeoutSeconds or was canceled"
    elif 300 <= code < 400:
        message += "; the connector never follows redirects, so check accountIdentifier"
    return _failure(operation, kind, message)


def _empty(input_value: GetStatementResultInput) -> StatementResult:
    return StatementResult(statement_handle=input_value.statement_handle, partition=input_value.partition)


async def _request(
    client: Client, call: Call, operation: str, method: str, target: str, body: object | None = None
) -> tuple[int, object, dict[str, str]] | Failure:
    try:
        response = await client.send(call, method, target, body)
    except CredentialError as error:
        return _failure(operation, FailureKind.AUTHENTICATION, str(error))
    except ProviderTransportError:
        return _failure(
            operation,
            FailureKind.TRANSPORT,
            "the Snowflake SQL API request failed or its response could not be read",
        )
    except (ValueError, TypeError):
        return _failure(
            operation, FailureKind.LOCAL_DEFECT, "the Snowflake SQL API request could not be built"
        )
    headers = dict(response.headers)
    try:
        body_bytes = await read_bounded_body(response, 256 << 20 if response.status_code == 200 else 1 << 20)
    except BodyTooLargeError:
        return _failure(
            operation, FailureKind.RESPONSE_TOO_LARGE, "the Snowflake response exceeds 268435456 bytes"
        )
    except ProviderTransportError:
        return _failure(
            operation,
            FailureKind.TRANSPORT,
            "the Snowflake SQL API request failed or its response could not be read",
        )
    try:
        document = json.loads(body_bytes) if body_bytes else {}
    except (UnicodeDecodeError, json.JSONDecodeError):
        if response.status_code == 200:
            return _failure(
                operation, FailureKind.PROTOCOL, "Snowflake returned a response the connector cannot decode"
            )
        document = {}
    return response.status_code, document, headers


def _classify(
    client: Client, operation: str, status_code: int, document: object, headers: dict[str, str]
) -> tuple[Failure, bool, float]:
    if status_code == 429:
        failure = _failure(operation, FailureKind.RATE_LIMIT, f"Snowflake returned HTTP {status_code}")
        return failure, True, parse_retry_after(headers.get("retry-after", ""), client.now())
    if status_code in (500, 502, 503, 504):
        failure = _failure(operation, FailureKind.AVAILABILITY, f"Snowflake returned HTTP {status_code}")
        return failure, True, parse_retry_after(headers.get("retry-after", ""), client.now())
    return _rejection(operation, status_code, document), False, 0


class SubmitStatementOperation:
    """Submit one parameterized statement through a keyed Mutation."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated operation identity and branches."""
        return definitions.SUBMIT_STATEMENT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: SubmitStatementInput) -> IdempotencyKey:
        """Use the stable Dex Call ID as Snowflake requestId."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: SubmitStatementInput) -> MutationAttempt[StatementSubmission]:
        """Send a single asynchronous SQL API submission."""
        operation = "submitStatement"
        try:
            bindings = _bind_statement(input.statement, input.parameters)
        except ValueError as error:
            return MutationAttempt.branch_result(
                definitions.DEFECT,
                StatementSubmission(),
                Receipt(),
                _failure(operation, FailureKind.VALIDATION, str(error)),
            )
        if not _HANDLE.fullmatch(str(call.idempotency_key)):
            return MutationAttempt.branch_result(
                definitions.DEFECT,
                StatementSubmission(),
                Receipt(),
                _failure(
                    operation,
                    FailureKind.LOCAL_DEFECT,
                    "the Step's idempotency key is not a UUID, so it cannot be Snowflake's requestId",
                ),
            )
        config = self.client.config
        body = {
            "statement": input.statement,
            "timeout": config.statement_timeout_seconds,
            "parameters": {"MULTI_STATEMENT_COUNT": "1"},
        }
        for name in ("database", "schema", "warehouse", "role"):
            value = getattr(config, name)
            if value:
                body[name] = value
        if bindings:
            body["bindings"] = bindings
        query = urlencode({"requestId": str(call.idempotency_key), "retry": "true", "async": "true"})
        answer = await _request(self.client, call, operation, "POST", "/api/v2/statements?" + query, body)
        if isinstance(answer, Failure):
            return _mutation_request_failure(operation, answer, StatementSubmission())
        code, document, headers = answer
        if code in (200, 202):
            status = _status(document)
            handle = str(status.get("statementHandle") or "")
            if not handle:
                return MutationAttempt.retry(
                    _failure(
                        operation,
                        FailureKind.PROTOCOL,
                        "Snowflake accepted the request but its response held no readable statement handle",
                    )
                )
            return MutationAttempt.branch_result(
                definitions.SUBMITTED,
                StatementSubmission(handle, _created_at(status)),
                _receipt(self.client, call, handle, status),
            )
        return _mutation_http_failure(
            self.client, call, operation, code, document, headers, StatementSubmission()
        )


class GetStatementResultOperation:
    """Read status and one bounded result partition as a Query."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated operation identity and branches."""
        return definitions.GET_STATEMENT_RESULT_DEFINITION

    async def invoke(self, call: Call, input: GetStatementResultInput) -> QueryAttempt[StatementResult]:
        """Read one result partition without waiting for query completion."""
        operation = "getStatementResult"
        empty = _empty(input)
        if not _HANDLE.fullmatch(input.statement_handle) or input.partition < 0:
            return QueryAttempt.branch_result(
                definitions.DEFECT,
                _empty(GetStatementResultInput()),
                Receipt(),
                _failure(operation, FailureKind.VALIDATION, "statementHandle or partition is invalid"),
            )
        if input.partition and (
            not input.columns
            or len(
                {column.name if isinstance(column, Column) else column["name"] for column in input.columns}
            )
            != len(input.columns)
        ):
            return QueryAttempt.branch_result(
                definitions.DEFECT,
                empty,
                Receipt(),
                _failure(
                    operation, FailureKind.VALIDATION, "columns from partition 0 must be unique and nonempty"
                ),
            )
        target = "/api/v2/statements/" + input.statement_handle
        if input.partition:
            target += "?partition=" + str(input.partition)
        answer = await _request(self.client, call, operation, "GET", target)
        if isinstance(answer, Failure):
            return _query_request_failure(operation, answer, empty)
        code, document, headers = answer
        status = _status(document)
        receipt = _receipt(self.client, call, input.statement_handle, status)
        if code == 202:
            return QueryAttempt.branch_result(
                definitions.RUNNING,
                StatementResult(
                    input.statement_handle, (), (), input.partition, submitted_at=_created_at(status)
                ),
                receipt,
            )
        if code != 200:
            failure, retry, delay = _classify(self.client, operation, code, document, headers)
            return (
                QueryAttempt.retry(failure, delay)
                if retry
                else QueryAttempt.branch_result(definitions.PROVIDER_REJECTED, empty, receipt, failure)
            )
        try:
            result = _decode_result(document, input, self.client)
        except DuplicateColumnError as error:
            return QueryAttempt.branch_result(
                definitions.DEFECT, empty, receipt,
                _failure(operation, FailureKind.VALIDATION, str(error)),
            )
        except ValueError as error:
            return QueryAttempt.branch_result(
                definitions.INVALID_RESPONSE,
                empty,
                receipt,
                _failure(operation, FailureKind.PROTOCOL, str(error)),
            )
        if result.truncated:
            return QueryAttempt.branch_result(
                definitions.TRUNCATED,
                result,
                receipt,
                _failure(
                    operation,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "the partition held more rows or bytes than maxRows or maxResponseBytes allow",
                ),
            )
        return QueryAttempt.branch_result(definitions.COMPLETED, result, receipt)


class CancelStatementOperation:
    """Cancel a submitted statement as a keyed Mutation."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated operation identity and branches."""
        return definitions.CANCEL_STATEMENT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CancelStatementInput) -> IdempotencyKey:
        """Use the stable Dex Call ID as Snowflake requestId."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: CancelStatementInput) -> MutationAttempt[StatementCancellation]:
        """Send one cancellation request for a valid statement handle."""
        operation = "cancelStatement"
        if not _HANDLE.fullmatch(input.statement_handle):
            return MutationAttempt.branch_result(
                definitions.DEFECT,
                StatementCancellation(),
                Receipt(),
                _failure(
                    operation,
                    FailureKind.VALIDATION,
                    "statementHandle must be the UUID-shaped handle submitStatement returned",
                ),
            )
        target = (
            "/api/v2/statements/"
            + input.statement_handle
            + "/cancel?"
            + urlencode({"requestId": str(call.idempotency_key)})
        )
        answer = await _request(self.client, call, operation, "POST", target, {})
        if isinstance(answer, Failure):
            return _mutation_request_failure(operation, answer, StatementCancellation())
        code, document, headers = answer
        status = _status(document)
        receipt = _receipt(self.client, call, input.statement_handle, status)
        if code == 200:
            return MutationAttempt.branch_result(
                definitions.CANCELED, StatementCancellation(input.statement_handle), receipt
            )
        return _mutation_http_failure(
            self.client, call, operation, code, document, headers, StatementCancellation(), receipt
        )


def _mutation_request_failure(operation: str, failure: Failure, empty: Output) -> MutationAttempt[Output]:
    if failure.kind in (FailureKind.TRANSPORT, FailureKind.RESPONSE_TOO_LARGE, FailureKind.PROTOCOL):
        return MutationAttempt.retry(failure)
    return MutationAttempt.branch_result(definitions.DEFECT, empty, Receipt(), failure)


def _query_request_failure(
    operation: str, failure: Failure, empty: StatementResult
) -> QueryAttempt[StatementResult]:
    if failure.kind == FailureKind.TRANSPORT:
        return QueryAttempt.retry(failure)
    branch = (
        definitions.INVALID_RESPONSE
        if failure.kind in (FailureKind.RESPONSE_TOO_LARGE, FailureKind.PROTOCOL)
        else definitions.DEFECT
    )
    return QueryAttempt.branch_result(branch, empty, Receipt(), failure)


def _mutation_http_failure(
    client: Client,
    call: Call,
    operation: str,
    code: int,
    document: object,
    headers: dict[str, str],
    empty: Output,
    receipt: Receipt | None = None,
) -> MutationAttempt[Output]:
    failure, retry, delay = _classify(client, operation, code, document, headers)
    if retry:
        return MutationAttempt.retry(failure, delay)
    status = _status(document)
    receipt = receipt or _receipt(client, call, str(status.get("statementHandle") or ""), status)
    return MutationAttempt.branch_result(definitions.PROVIDER_REJECTED, empty, receipt, failure)


def _bind_statement(statement: str, parameters: tuple[object, ...]) -> dict[str, dict[str, object]]:
    if not statement.strip() or len(statement.encode()) > 1 << 20 or "\x00" in statement:
        raise ValueError("statement is required and must be valid SQL text under 1 MB")
    if len(parameters) > 10000:
        raise ValueError("statement cannot bind more than 10000 parameters")
    count = _placeholder_count(statement)
    if count != len(parameters):
        raise ValueError(f"statement uses {count} ? placeholders but {len(parameters)} parameters were bound")
    bindings: dict[str, dict[str, object]] = {}
    total = 0
    for index, parameter in enumerate(parameters, 1):
        if parameter is None:
            kind, value = "TEXT", None
        elif isinstance(parameter, str):
            if "\x00" in parameter:
                raise ValueError("text must not contain NUL characters")
            kind, value = "TEXT", parameter
        elif isinstance(parameter, bool):
            kind, value = "BOOLEAN", str(parameter).lower()
        elif isinstance(parameter, int):
            kind, value = "FIXED", str(parameter)
        elif isinstance(parameter, float):
            if not math.isfinite(parameter):
                raise ValueError("float must be finite")
            value = next(
                formatted for precision in range(1, 18)
                if float(formatted := format(parameter, f".{precision}g")) == parameter
            )
            kind = "REAL"
        elif isinstance(parameter, Decimal):
            value = str(parameter)
            if not _JSON_NUMBER.fullmatch(value):
                raise ValueError("decimal parameter is not a JSON number")
            kind = "FIXED" if re.fullmatch(r"-?(?:0|[1-9][0-9]*)", value) else "TEXT"
        elif isinstance(parameter, (dict, list)):
            kind, value = "TEXT", json.dumps(parameter, separators=(",", ":"), ensure_ascii=False)
        elif isinstance(parameter, bytes):
            kind, value = "BINARY", parameter.hex()
        elif isinstance(parameter, datetime):
            if parameter.tzinfo is None:
                raise ValueError("timestamp must have a timezone")
            if not 1678 <= parameter.year <= 2261:
                raise ValueError("timestamp must be between the years 1678 and 2261")
            offset = parameter.utcoffset() or timedelta()
            utc_value = parameter.astimezone(UTC)
            since_epoch = utc_value - datetime(1970, 1, 1, tzinfo=UTC)
            nanoseconds = (
                (since_epoch.days * 86400 + since_epoch.seconds) * 1_000_000_000
                + since_epoch.microseconds * 1000
            )
            kind, value = (
                "TIMESTAMP_TZ",
                f"{nanoseconds} {1440 + int(offset.total_seconds() / 60)}",
            )
        else:
            raise ValueError("unsupported statement parameter type")
        if value is not None:
            total += len(value.encode())
        if total > 16 << 20:
            raise ValueError("parameters exceed 16777216 bytes combined")
        bindings[str(index)] = {"type": kind, "value": value}
    return bindings


def _placeholder_count(statement: str) -> int:
    count = 0
    index = 0
    while index < len(statement):
        char = statement[index]
        if char == "?":
            count += 1
        elif char in ("'", '"'):
            quote = char
            index += 1
            while index < len(statement):
                if statement[index] == "\\" and quote == "'":
                    index += 2
                    continue
                if statement[index] == quote:
                    if index + 1 < len(statement) and statement[index + 1] == quote:
                        index += 2
                        continue
                    break
                index += 1
            else:
                raise ValueError("statement has an unterminated quoted value")
        elif statement.startswith("$$", index):
            end = statement.find("$$", index + 2)
            if end < 0:
                raise ValueError("statement has an unterminated $$ string")
            index = end + 1
        elif statement.startswith(("--", "//"), index):
            end = statement.find("\n", index)
            if end < 0:
                return count
            index = end
        elif statement.startswith("/*", index):
            end = statement.find("*/", index + 2)
            if end < 0:
                raise ValueError("statement has an unterminated /* comment")
            index = end + 1
        index += 1
    return count


def _decode_result(document: object, input: GetStatementResultInput, client: Client) -> StatementResult:
    if isinstance(document, list):
        raw_rows = document
        status = {}
        metadata = None
        stats = None
    elif isinstance(document, dict):
        raw_rows = document.get("data") or []
        status = _status(document)
        metadata = document.get("resultSetMetaData")
        stats = document.get("stats")
    else:
        raise ValueError("Snowflake returned a response the connector cannot decode")
    if not isinstance(raw_rows, list):
        raise ValueError("Snowflake returned a response the connector cannot decode")
    columns = tuple(Column(**column) if isinstance(column, dict) else column for column in input.columns)
    partition_count = 0
    total_row_count = None
    if input.partition == 0:
        if not isinstance(metadata, dict) or metadata.get("format", "") not in ("", "jsonv2"):
            raise ValueError("Snowflake returned partition 0 without jsonv2 column metadata")
        row_types = metadata.get("rowType") or []
        if not isinstance(row_types, list):
            raise ValueError("Snowflake returned invalid column metadata")
        described = []
        for row_type in row_types:
            if not isinstance(row_type, dict) or not row_type.get("name") or not row_type.get("type"):
                raise ValueError("Snowflake returned invalid column metadata")
            described.append(
                Column(
                    name=row_type["name"],
                    type=row_type["type"].lower(),
                    precision=row_type.get("precision") or 0,
                    scale=row_type.get("scale") or 0,
                    length=row_type.get("length") or 0,
                    nullable=row_type.get("nullable") or False,
                )
            )
        columns = tuple(described)
        if len({column.name for column in columns}) != len(columns):
            raise DuplicateColumnError(
                "the statement returns a column more than once; give each result column a unique alias"
            )
        partition_count = len(metadata.get("partitionInfo") or [])
        total_row_count = metadata.get("numRows")
    rows: list[dict[str, object]] = []
    encoded_bytes = 2
    truncated = False
    for raw_row in raw_rows:
        if len(rows) >= client.config.max_rows:
            truncated = True
            break
        if not isinstance(raw_row, list) or len(raw_row) != len(columns):
            raise ValueError("Snowflake returned a row whose width differs from its columns")
        row = {}
        for column, text in zip(columns, raw_row, strict=True):
            try:
                row[column.name] = _decode_value(column, text)
            except (ValueError, OverflowError, TypeError):
                raise ValueError(
                    f'column "{column.name}" of type {column.type} returned text the connector cannot decode'
                ) from None
        encoded_bytes += len(json.dumps(row, ensure_ascii=False, separators=(",", ":")).encode()) + 1
        if encoded_bytes > client.config.max_response_bytes:
            truncated = True
            break
        rows.append(row)
    described_stats = None
    if isinstance(stats, dict):
        described_stats = StatementStats(
            rows_inserted=stats.get("numRowsInserted") or 0,
            rows_updated=stats.get("numRowsUpdated") or 0,
            rows_deleted=stats.get("numRowsDeleted") or 0,
            duplicate_rows_updated=stats.get("numDuplicateRowsUpdated") or 0,
        )
    return StatementResult(
        statement_handle=input.statement_handle,
        columns=columns,
        rows=tuple(rows),
        partition=input.partition,
        partition_count=partition_count,
        total_row_count=total_row_count,
        truncated=truncated,
        stats=described_stats,
        submitted_at=_created_at(status),
    )


def _decode_value(column: Column, text: str | None) -> object:
    if text is None:
        return None
    if not isinstance(text, str):
        raise ValueError("non-text value")
    kind = column.type
    if kind == "fixed":
        if not _DECIMAL.fullmatch(text):
            raise ValueError("invalid fixed")
        return int(text) if column.scale == 0 and 0 < column.precision <= 15 else text
    if kind == "real":
        lowered = text.lower()
        if lowered in ("nan", "inf", "infinity", "-inf", "-infinity"):
            return {
                "nan": "NaN",
                "inf": "Infinity",
                "infinity": "Infinity",
                "-inf": "-Infinity",
                "-infinity": "-Infinity",
            }[lowered]
        value = float(text)
        if not math.isfinite(value):
            raise ValueError("invalid real")
        return value
    if kind == "decfloat":
        if not _DECFLOAT.fullmatch(text):
            raise ValueError("invalid decfloat")
        return text
    if kind == "boolean":
        if text in ("1", "t", "T", "TRUE", "true", "True"):
            return True
        if text in ("0", "f", "F", "FALSE", "false", "False"):
            return False
        raise ValueError("invalid boolean")
    if kind == "binary":
        if len(text) % 2 or not re.fullmatch(r"[0-9a-fA-F]*", text):
            raise ValueError("invalid binary hex")
        return base64.b64encode(bytes.fromhex(text)).decode()
    if kind in ("variant", "object", "array", "map"):
        def reject_constant(value: str) -> object:
            raise ValueError("invalid semi-structured JSON")

        return json.loads(text, parse_constant=reject_constant)
    if kind == "date":
        days = int(text)
        if not re.fullmatch(r"-?[0-9]{1,9}", text):
            raise ValueError("invalid date")
        try:
            return (datetime(1970, 1, 1, tzinfo=UTC) + timedelta(days=days)).date().isoformat()
        except (OverflowError, ValueError):
            return text
    if kind in ("time", "timestamp_ntz", "timestamp_ltz", "timestamp_tz"):
        seconds_text = text
        offset_minutes = 0
        if kind == "timestamp_tz":
            pieces = text.split(" ")
            if len(pieces) != 2 or not re.fullmatch(r"[0-9]{1,4}", pieces[1]):
                raise ValueError("invalid timezone offset")
            offset_minutes = int(pieces[1]) - 1440
            if not -1440 <= offset_minutes <= 1440:
                raise ValueError("timezone offset out of range")
            seconds_text = pieces[0]
        match = _EPOCH.fullmatch(seconds_text)
        if not match:
            raise ValueError("invalid epoch seconds")
        seconds = int(match[2])
        fraction = int((match[3] or "").ljust(9, "0"))
        total_nanoseconds = (-1 if match[1] else 1) * (seconds * 1_000_000_000 + fraction)
        unix_seconds, nanoseconds = divmod(total_nanoseconds, 1_000_000_000)
        try:
            instant = datetime.fromtimestamp(unix_seconds, UTC)
        except (OverflowError, OSError, ValueError):
            return text
        fraction_text = f".{nanoseconds:09d}".rstrip("0") if nanoseconds else ""
        if kind == "time":
            return instant.strftime("%H:%M:%S") + fraction_text
        if kind == "timestamp_ntz":
            return instant.strftime("%Y-%m-%dT%H:%M:%S") + fraction_text
        if kind == "timestamp_tz":
            zoned = instant + timedelta(minutes=offset_minutes)
            sign = "+" if offset_minutes >= 0 else "-"
            absolute_minutes = abs(offset_minutes)
            suffix = f"{sign}{absolute_minutes // 60:02d}:{absolute_minutes % 60:02d}"
            return zoned.strftime("%Y-%m-%dT%H:%M:%S") + fraction_text + suffix
        return instant.strftime("%Y-%m-%dT%H:%M:%S") + fraction_text + "Z"
    return text
