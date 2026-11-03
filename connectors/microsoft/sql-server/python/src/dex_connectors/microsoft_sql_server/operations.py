# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""SQL Server Query and Mutation invocation and native-driver classification."""

import asyncio
import base64
import json
import math
import re
import socket
import ssl
import struct
import uuid
from datetime import UTC, date, datetime, time
from decimal import Decimal
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
    ReauthorizationRequiredError,
    Receipt,
)

from ._generated.definitions import (
    COMPLETED,
    CONNECTOR_ID,
    DEFECT,
    EXECUTE_DEFINITION,
    EXECUTE_OPERATION_ID,
    INVALID_RESPONSE,
    LIMIT_EXCEEDED,
    PROVIDER_REJECTED,
    QUERY_DEFINITION,
    QUERY_OPERATION_ID,
    TRUNCATED,
)
from .models import Column, ExecuteStatementInput, QueryRowsInput, RowSet, StatementExecution

_RETRY_CONFLICT = {1205, 1222, 3960, 3961}
_RETRY_AVAILABILITY = {
    615,
    701,
    926,
    1101,
    1105,
    4221,
    6005,
    8628,
    8645,
    8651,
    9002,
    10922,
    10928,
    10929,
    10936,
    18401,
    40197,
    40501,
    40613,
    49918,
    49919,
    49920,
}
_AUTHENTICATION = {18452, 18456, 18470, 18486, 18487, 18488, 40532, 40615}
_AUTHORIZATION = {229, 230, 262, 297, 916, 3906}
_NOT_FOUND = {207, 208, 2812, 4060, 4064, 4104}
_CONFLICT = {515, 547, 2601, 2627, 3902, 3903}
_VALIDATION = {102, 137, 156, 241, 242, 245, 266, 334, 574, 2628, 8003, 8114, 8115, 8134, 8152}
_SESSION_LIMIT = {40549, 40550, 40551, 40552, 40553}
_COMMIT_ROLLED_BACK = {1205, 3902, 3903}
_DENIED_WORDS = {
    "BEGIN",
    "COMMIT",
    "ROLLBACK",
    "SAVE",
    "TRAN",
    "TRANSACTION",
    "DISTRIBUTED",
    "EXEC",
    "EXECUTE",
    "USE",
    "SETUSER",
    "REVERT",
    "IF",
    "WHILE",
    "GOTO",
    "RETURN",
    "BREAK",
    "CONTINUE",
    "WAITFOR",
    "DECLARE",
    "DEALLOCATE",
    "CREATE",
    "ALTER",
    "DROP",
    "TRUNCATE",
    "GRANT",
    "DENY",
    "REVOKE",
    "TRIGGER",
    "STATISTICS",
    "BACKUP",
    "RESTORE",
    "DBCC",
    "KILL",
    "SHUTDOWN",
    "RECONFIGURE",
    "CHECKPOINT",
    "LOAD",
    "DUMP",
    "BULK",
    "OPENROWSET",
    "OPENDATASOURCE",
    "OPENQUERY",
    "ROWCOUNT",
    "TEXTSIZE",
    "IDENTITY_INSERT",
    "PRINT",
    "RAISERROR",
    "READTEXT",
    "WRITETEXT",
    "UPDATETEXT",
}
_POSITIONAL = re.compile(r"@p([1-9][0-9]*)\Z")
_SQL_TYPES = {
    48: "TINYINT",
    52: "SMALLINT",
    56: "INT",
    127: "BIGINT",
    50: "BIT",
    59: "REAL",
    62: "FLOAT",
    106: "DECIMAL",
    108: "DECIMAL",
    60: "MONEY",
    122: "SMALLMONEY",
    39: "VARCHAR",
    47: "CHAR",
    167: "VARCHAR",
    175: "CHAR",
    231: "NVARCHAR",
    239: "NCHAR",
    35: "TEXT",
    99: "NTEXT",
    241: "XML",
    37: "VARBINARY",
    45: "BINARY",
    165: "VARBINARY",
    173: "BINARY",
    34: "IMAGE",
    36: "UNIQUEIDENTIFIER",
    40: "DATE",
    41: "TIME",
    42: "DATETIME2",
    43: "DATETIMEOFFSET",
    58: "SMALLDATETIME",
    61: "DATETIME",
    98: "SQL_VARIANT",
}


class _InvalidResponse(Exception):
    pass


def _scan_sql(statement: str) -> list[tuple[str, str, int, int]]:
    tokens: list[tuple[str, str, int, int]] = []
    offset = 0
    while offset < len(statement):
        start = offset
        character = statement[offset]
        if character.isspace():
            offset += 1
        elif statement.startswith("--", offset):
            newline = re.search(r"[\r\n]", statement[offset:])
            offset = len(statement) if newline is None else offset + newline.start()
        elif statement.startswith("/*", offset):
            depth = 1
            offset += 2
            while depth and offset < len(statement):
                if statement.startswith("/*", offset):
                    depth += 1
                    offset += 2
                elif statement.startswith("*/", offset):
                    depth -= 1
                    offset += 2
                else:
                    offset += 1
            if depth:
                raise ValueError("statement has an unterminated block comment")
        elif character in ("'", '"', "[") or (
            character in "Nn" and statement[offset + 1 : offset + 2] == "'"
        ):
            if character in "Nn":
                offset += 1
                character = "'"
            closing = "]" if character == "[" else character
            offset += 1
            while offset < len(statement):
                if statement[offset] == closing:
                    offset += 1
                    if statement[offset : offset + 1] == closing:
                        offset += 1
                    else:
                        break
                else:
                    offset += 1
            else:
                raise ValueError("statement has an unterminated literal or identifier")
        elif character == "@":
            offset += 1
            while offset < len(statement) and (statement[offset].isalnum() or statement[offset] in "_#$@"):
                offset += 1
            tokens.append(("variable", statement[start:offset], start, offset))
        elif character.isalpha() or character in "_#$":
            offset += 1
            while offset < len(statement) and (statement[offset].isalnum() or statement[offset] in "_#$@"):
                offset += 1
            tokens.append(("word", statement[start:offset], start, offset))
        elif character == ";":
            offset += 1
            tokens.append(("semicolon", ";", start, offset))
        elif character == "(":
            offset += 1
            tokens.append(("open", "(", start, offset))
        else:
            offset += 1
            tokens.append(("other", character, start, offset))
    return tokens


def _bind_statement(
    statement: str, parameters: tuple[Any, ...], write: bool, key: str | None = None
) -> tuple[str, dict[str, Any]]:
    try:
        statement_bytes = statement.encode("utf-8")
    except UnicodeEncodeError as error:
        raise ValueError("statement must be valid UTF-8") from error
    if not statement.strip() or len(statement_bytes) > 1 << 20 or "\x00" in statement:
        raise ValueError("statement is empty, oversized, or contains NUL")
    if len(parameters) + (key is not None) > 2098:
        raise ValueError("statement cannot bind more than 2098 parameters")
    tokens = _scan_sql(statement)
    first = next((text.upper() for kind, text, _, _ in tokens if kind != "open"), "")
    allowed = {"INSERT", "UPDATE", "DELETE", "MERGE", "WITH"} if write else {"SELECT", "WITH"}
    if first not in allowed:
        raise ValueError("statement starts with an operation this connector does not allow")
    if re.fullmatch(r"[\w.#\[\]]+", statement):
        raise ValueError("statement would be sent as a stored procedure call")
    seen: set[int] = set()
    final_semicolon = False
    for index, (kind, text, _, _) in enumerate(tokens):
        if kind == "semicolon":
            final_semicolon = True
            if any(next_kind != "semicolon" for next_kind, _, _, _ in tokens[index + 1 :]):
                raise ValueError("statement holds more than one statement")
        elif final_semicolon:
            raise ValueError("statement holds more than one statement")
        if kind == "word" and text.upper() in _DENIED_WORDS:
            if (
                text.upper() != "USE"
                or index + 1 >= len(tokens)
                or tokens[index + 1][1].upper() not in ("HINT", "PLAN")
            ):
                raise ValueError(f"statement uses reserved keyword {text.upper()}")
        if kind == "variable" and not text.startswith("@@"):
            match = _POSITIONAL.fullmatch(text)
            if match is None:
                raise ValueError("statement references a variable other than lowercase @pN")
            position = int(match.group(1))
            if position > len(parameters) + (key is not None):
                raise ValueError("statement references an unbound parameter")
            seen.add(position)
    if seen != set(range(1, len(parameters) + (key is not None) + 1)):
        raise ValueError("each bound parameter must be referenced in the statement")
    values: dict[str, Any] = {}
    total_bytes = 0
    for index, parameter in enumerate((*parameters, *((key,) if key is not None else ())), 1):
        if isinstance(parameter, bool) or parameter is None:
            size = 1 if parameter is not None else 0
        elif isinstance(parameter, int) and -(1 << 63) <= parameter < 1 << 64:
            size = 8
        elif isinstance(parameter, float) and math.isfinite(parameter):
            size = 8
        elif isinstance(parameter, (str, bytes)):
            try:
                size = len(parameter.encode("utf-8") if isinstance(parameter, str) else parameter)
            except UnicodeEncodeError as error:
                raise ValueError(f"parameter @p{index} must be valid UTF-8") from error
        elif isinstance(parameter, datetime):
            size = 10
        else:
            raise ValueError(f"parameter @p{index} has an unsupported type or value")
        total_bytes += size
        values[f"p{index}"] = parameter
    if total_bytes > 16 << 20:
        raise ValueError("parameters exceed 16777216 bytes combined")
    # Escape literal percent signs for the DB-API pyformat parser, then insert named binds.
    chunks: list[str] = []
    offset = 0
    for kind, text, start, end in tokens:
        if kind == "variable" and not text.startswith("@@"):
            chunks.append(statement[offset:start].replace("%", "%%"))
            chunks.append(f"%({text[1:]})s")
            offset = end
    chunks.append(statement[offset:].replace("%", "%%"))
    return "".join(chunks), values


def _classify(error: BaseException, phase: str) -> tuple[str, FailureKind, str, dict[str, str]]:
    number = getattr(error, "number", 0)
    if isinstance(number, int) and number != 0:
        metadata = {
            "errorNumber": str(number),
            "errorState": str(getattr(error, "state", 0)),
            "errorSeverity": str(getattr(error, "severity", 0)),
        }
        if phase == "commit" and number not in _COMMIT_ROLLED_BACK:
            return "uncertain", FailureKind.AVAILABILITY, "SQL Server commit outcome is unknown", metadata
        if number in _RETRY_CONFLICT:
            return "retry", FailureKind.CONFLICT, "SQL Server transaction conflict; retry", metadata
        if (
            number in _RETRY_AVAILABILITY
            or getattr(error, "severity", 0) >= 20
            and number not in (_SESSION_LIMIT | {40544})
        ):
            return "retry", FailureKind.AVAILABILITY, "SQL Server is temporarily unavailable", metadata
        for numbers, failure_kind in (
            (_AUTHENTICATION, FailureKind.AUTHENTICATION),
            (_AUTHORIZATION, FailureKind.AUTHORIZATION),
            (_NOT_FOUND, FailureKind.NOT_FOUND),
            (_CONFLICT, FailureKind.CONFLICT),
            (_VALIDATION, FailureKind.VALIDATION),
            ({40544}, FailureKind.QUOTA_EXHAUSTED),
            (_SESSION_LIMIT, FailureKind.AVAILABILITY),
        ):
            if number in numbers:
                return "rejected", failure_kind, f"SQL Server rejected request with error {number}", metadata
        return (
            "rejected",
            FailureKind.PROVIDER_REJECTION,
            f"SQL Server rejected request with error {number}",
            metadata,
        )
    if isinstance(error, _InvalidResponse):
        return "invalid", FailureKind.PROTOCOL, str(error), {}
    error_text = str(error).lower()
    if (
        isinstance(error, ssl.SSLError)
        or "certificate" in type(error).__name__.lower()
        or "certificate does not match host name" in error_text
        or "certificate verify failed" in error_text
    ):
        return "rejected", FailureKind.AUTHENTICATION, "SQL Server certificate was rejected", {}
    if isinstance(error, ValueError):
        return "defect", FailureKind.VALIDATION, str(error), {}
    if phase == "commit":
        return "uncertain", FailureKind.TRANSPORT, "SQL Server commit outcome is unknown", {}
    if isinstance(error, TimeoutError) and phase == "statement":
        return (
            "rejected",
            FailureKind.AVAILABILITY,
            "SQL Server statement timed out; nothing was committed",
            {},
        )
    if phase == "connect":
        cause: BaseException | None = error
        while cause is not None:
            if isinstance(cause, socket.gaierror) and cause.errno == socket.EAI_NONAME:
                return "rejected", FailureKind.NOT_FOUND, "SQL Server host was not found", {}
            cause = cause.__cause__ or cause.__context__
        if isinstance(error, socket.gaierror) and error.errno == socket.EAI_NONAME:
            return "rejected", FailureKind.NOT_FOUND, "SQL Server host was not found", {}
        return "retry", FailureKind.AVAILABILITY, "SQL Server is unreachable or did not finish connecting", {}
    if isinstance(error, (OSError, EOFError, ConnectionError, TimeoutError)):
        return "retry", FailureKind.TRANSPORT, "SQL Server connection failed before commit", {}
    return "invalid", FailureKind.PROTOCOL, "SQL Server driver could not decode the reply", {}


def _receipt(call: Call, metadata: dict[str, str] | None = None) -> Receipt:
    return Receipt(
        CallID(str(call.id)),
        CONNECTOR_ID,
        datetime.now(UTC),
        IdempotencyKey(str(call.idempotency_key)),
        metadata=metadata or {},
    )


def _row_value(value: Any, type_name: str) -> Any:
    if value is None:
        return None
    if type_name == "BIGINT" and isinstance(value, int):
        return str(value)
    if type_name in ("DECIMAL", "MONEY", "SMALLMONEY") and isinstance(value, (Decimal, str)):
        return format(value, "f") if isinstance(value, Decimal) else value
    if type_name in ("TINYINT", "SMALLINT", "INT") and type(value) is int:
        return value
    if type_name == "BIT" and isinstance(value, bool):
        return value
    if type_name in ("CHAR", "VARCHAR", "TEXT", "NCHAR", "NVARCHAR", "NTEXT", "XML") and isinstance(
        value, str
    ):
        return value
    if type_name in ("REAL", "FLOAT") and isinstance(value, float):
        if not math.isfinite(value):
            raise _InvalidResponse("SQL Server returned a nonfinite number")
        if type_name == "REAL":
            packed = struct.pack("!f", value)
            for digits in range(1, 10):
                candidate = float(format(value, f".{digits}g"))
                if struct.pack("!f", candidate) == packed:
                    return candidate
        return value
    if type_name in ("BINARY", "VARBINARY", "IMAGE") and isinstance(value, bytes):
        return base64.b64encode(value).decode("ascii")
    if type_name == "UNIQUEIDENTIFIER" and isinstance(value, uuid.UUID):
        return str(value)
    if type_name in ("DATETIME", "SMALLDATETIME", "DATETIME2", "DATETIMEOFFSET") and isinstance(
        value, datetime
    ):
        if type_name != "DATETIMEOFFSET" and value.tzinfo is not None:
            value = value.replace(tzinfo=None)
        formatted = value.isoformat(timespec="microseconds")
        formatted = re.sub(r"(\.\d*?)0+(?=[+-]\d\d:\d\d$|$)", r"\1", formatted)
        formatted = re.sub(r"\.(?=[+-]\d\d:\d\d$|$)", "", formatted)
        return (
            formatted[:-6] + "Z"
            if type_name == "DATETIMEOFFSET" and formatted.endswith("+00:00")
            else formatted
        )
    if type_name == "DATE" and isinstance(value, date):
        return value.isoformat()
    if type_name == "TIME" and isinstance(value, time):
        return value.isoformat(timespec="microseconds").rstrip("0").rstrip(".")
    raise _InvalidResponse("SQL Server returned a value the connector cannot decode")


def _read_rows(
    cursor: Any, max_rows: int, max_bytes: int
) -> tuple[tuple[Column, ...], tuple[dict[str, Any], ...], bool]:
    description = cursor.description
    if not description:
        return (), (), False
    columns: list[Column] = []
    for field in description:
        name = str(field[0])
        if not name or any(column.name == name for column in columns):
            raise ValueError("SQL Server returned duplicate or empty column names")
        type_name = (
            _SQL_TYPES.get(field[1], "UNKNOWN") if isinstance(field[1], int) else str(field[1]).upper()
        )
        if type_name in ("TIME", "DATETIME2", "DATETIMEOFFSET"):
            temporal_scale = (
                field[5]
                if len(field) > 5 and isinstance(field[5], int)
                else (field[4] if len(field) > 4 else None)
            )
            if temporal_scale == 7:
                raise _InvalidResponse(
                    f'column "{name}" of type {type_name} uses scale 7; '
                    "the Python driver cannot decode it exactly"
                )
        columns.append(Column(name, type_name))
    rows: list[dict[str, Any]] = []
    encoded_bytes = 2
    while True:
        raw = cursor.fetchone()
        if raw is None:
            if callable(getattr(cursor, "nextset", None)) and cursor.nextset():
                raise ValueError("statement returned more than one result set")
            return tuple(columns), tuple(rows), False
        if len(rows) >= max_rows:
            return tuple(columns), tuple(rows), True
        row = {
            column.name: _row_value(value, column.type_name)
            for column, value in zip(columns, raw, strict=True)
        }
        encoded_bytes += len(json.dumps(row, separators=(",", ":"), ensure_ascii=False).encode("utf-8")) + (
            1 if rows else 0
        )
        if encoded_bytes > max_bytes:
            return tuple(columns), tuple(rows), True
        rows.append(row)


class QueryRowsOperation:
    """Run one read statement and roll back its transaction."""

    def __init__(self, client: Any) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest Query identity and branch contract."""
        return QUERY_DEFINITION

    async def invoke(self, call: Call, input: QueryRowsInput) -> QueryAttempt[RowSet]:
        """Classify one native read attempt without persisting credentials."""
        try:
            statement, parameters = _bind_statement(input.statement, input.parameters, False)
        except ValueError as error:
            return QueryAttempt.branch_result(
                DEFECT,
                RowSet(),
                Receipt(),
                Failure(FailureKind.VALIDATION, CONNECTOR_ID, QUERY_OPERATION_ID, str(error)),
            )
        try:
            credentials = await self.client.credentials.resolve(call)
            password = credentials.password.reveal()
            if not password:
                raise ValueError("password is unavailable")
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            return QueryAttempt.branch_result(
                DEFECT,
                RowSet(),
                Receipt(),
                Failure(
                    FailureKind.AUTHENTICATION,
                    CONNECTOR_ID,
                    QUERY_OPERATION_ID,
                    "SQL Server connection credentials are unavailable",
                ),
            )
        return await asyncio.to_thread(self._invoke_sync, call, statement, parameters, password)

    def _invoke_sync(
        self, call: Call, statement: str, parameters: dict[str, Any], password: str
    ) -> QueryAttempt[RowSet]:
        phase = "connect"
        connection = None
        try:
            connection = self.client.open_connection(password)
            phase = "statement"
            cursor = connection.cursor()
            cursor.execute(statement, parameters)
            columns, rows, truncated = _read_rows(
                cursor, self.client.config.max_rows, self.client.config.max_response_bytes
            )
            if not columns:
                raise ValueError("query statement returned no result set")
            value = RowSet(columns, rows, truncated)
            if truncated:
                failure = Failure(
                    FailureKind.RESPONSE_TOO_LARGE,
                    CONNECTOR_ID,
                    QUERY_OPERATION_ID,
                    "SQL Server rows exceeded maxRows or maxResponseBytes",
                )
                return QueryAttempt.branch_result(TRUNCATED, value, _receipt(call), failure)
            return QueryAttempt.branch_result(COMPLETED, value, _receipt(call))
        except Exception as error:
            disposition, kind, message, metadata = _classify(error, phase)
            failure = Failure(kind, CONNECTOR_ID, QUERY_OPERATION_ID, message)
            if disposition in ("retry", "uncertain"):
                return QueryAttempt.retry(failure)
            branch = (
                DEFECT
                if disposition == "defect"
                else INVALID_RESPONSE
                if disposition == "invalid"
                else PROVIDER_REJECTED
            )
            return QueryAttempt.branch_result(branch, RowSet(), _receipt(call, metadata), failure)
        finally:
            if connection is not None:
                try:
                    connection.rollback()
                except Exception:
                    # Closing the session also abandons the read transaction.
                    pass
                try:
                    connection.close()
                except Exception:
                    # The read outcome was already classified and the session cannot be reused.
                    pass


class ExecuteStatementOperation:
    """Run one bounded write and classify commit uncertainty separately."""

    def __init__(self, client: Any) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest Mutation identity and branch contract."""
        return EXECUTE_DEFINITION

    def idempotency_key(self, call_id: CallID, _input: ExecuteStatementInput) -> IdempotencyKey:
        """Use the stable Dex Call ID for an optional final SQL placeholder."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: ExecuteStatementInput) -> MutationAttempt[StatementExecution]:
        """Run one write, rolling back every precommit failure or limit excess."""
        try:
            if input.max_rows_affected is not None and input.max_rows_affected < 0:
                raise ValueError("maxRowsAffected cannot be negative")
            if input.idempotency_key_placeholder not in (0, len(input.parameters) + 1):
                raise ValueError("idempotencyKeyPlaceholder must be the last placeholder")
            key = str(call.idempotency_key) if input.idempotency_key_placeholder else None
            statement, parameters = _bind_statement(input.statement, input.parameters, True, key)
        except ValueError as error:
            return MutationAttempt.branch_result(
                DEFECT,
                StatementExecution(),
                Receipt(),
                Failure(FailureKind.VALIDATION, CONNECTOR_ID, EXECUTE_OPERATION_ID, str(error)),
            )
        try:
            credentials = await self.client.credentials.resolve(call)
            password = credentials.password.reveal()
            if not password:
                raise ValueError("password is unavailable")
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            return MutationAttempt.branch_result(
                DEFECT,
                StatementExecution(),
                Receipt(),
                Failure(
                    FailureKind.AUTHENTICATION,
                    CONNECTOR_ID,
                    EXECUTE_OPERATION_ID,
                    "SQL Server connection credentials are unavailable",
                ),
            )
        return await asyncio.to_thread(self._invoke_sync, call, input, statement, parameters, password)

    def _invoke_sync(
        self,
        call: Call,
        input: ExecuteStatementInput,
        statement: str,
        parameters: dict[str, Any],
        password: str,
    ) -> MutationAttempt[StatementExecution]:
        phase = "connect"
        connection = None
        was_committed = False
        try:
            connection = self.client.open_connection(password)
            phase = "statement"
            cursor = connection.cursor()
            cursor.execute(statement, parameters)
            if input.returns_output_rows:
                columns, rows, truncated = _read_rows(
                    cursor, self.client.config.max_rows, self.client.config.max_response_bytes
                )
                if truncated:
                    failure = Failure(
                        FailureKind.RESPONSE_TOO_LARGE,
                        CONNECTOR_ID,
                        EXECUTE_OPERATION_ID,
                        "SQL Server OUTPUT rows exceeded bounds",
                    )
                    return MutationAttempt.branch_result(
                        LIMIT_EXCEEDED, StatementExecution(), _receipt(call), failure
                    )
                if not columns:
                    raise ValueError("returnsOutputRows is set but no OUTPUT result set was returned")
                value = StatementExecution(len(rows), columns, rows)
            else:
                value = StatementExecution(max(0, int(cursor.rowcount)))
            if input.max_rows_affected is not None and value.rows_affected > input.max_rows_affected:
                failure = Failure(
                    FailureKind.CONFLICT,
                    CONNECTOR_ID,
                    EXECUTE_OPERATION_ID,
                    "SQL Server rows affected exceeded maxRowsAffected",
                )
                return MutationAttempt.branch_result(
                    LIMIT_EXCEEDED, StatementExecution(), _receipt(call), failure
                )
            phase = "commit"
            connection.commit()
            was_committed = True
            return MutationAttempt.branch_result(COMPLETED, value, _receipt(call))
        except Exception as error:
            disposition, kind, message, metadata = _classify(error, phase)
            failure = Failure(kind, CONNECTOR_ID, EXECUTE_OPERATION_ID, message)
            if disposition == "retry":
                return MutationAttempt.retry(failure)
            if disposition == "uncertain":
                return MutationAttempt.uncertain(StatementExecution(), failure, _receipt(call, metadata))
            branch = (
                DEFECT
                if disposition == "defect"
                else INVALID_RESPONSE
                if disposition == "invalid"
                else PROVIDER_REJECTED
            )
            return MutationAttempt.branch_result(
                branch, StatementExecution(), _receipt(call, metadata), failure
            )
        finally:
            if connection is not None:
                if not was_committed:
                    try:
                        connection.rollback()
                    except Exception:
                        # Closing the session abandons an uncommitted transaction.
                        pass
                try:
                    connection.close()
                except Exception:
                    # Outcome was already classified and the native connection is unusable.
                    pass
