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
import threading
import time as clock_time
import unicodedata
import uuid
from collections.abc import Callable
from dataclasses import dataclass, replace
from datetime import UTC, date, datetime, time
from decimal import Decimal, localcontext
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
from dex_connectors.sdk.projectconfig.broker import ConnectionNotSelectedError, CredentialBrokerRejectedError
from dex_connectors.sdk.projectconfig.storage import ObjectNotFoundError
from dex_connectors.sdk.singledispatch import (
    CheckpointError,
    DecisionKind,
    DispatchNotAllowedError,
    Fence,
    InsufficientAttemptTimeError,
)
from OpenSSL import SSL
from pytds import tds_base  # type: ignore[import-untyped]

from ._generated.definitions import (
    COMPLETED,
    CONNECTOR_ID,
    DEFECT,
    EXECUTE_DEFINITION,
    EXECUTE_NON_IDEMPOTENT_DEFINITION,
    EXECUTE_NON_IDEMPOTENT_OPERATION_ID,
    EXECUTE_OPERATION_ID,
    INVALID_RESPONSE,
    LIMIT_EXCEEDED,
    PROVIDER_REJECTED,
    QUERY_DEFINITION,
    QUERY_OPERATION_ID,
    TRUNCATED,
)
from .models import Column, ExecuteStatementInput, QueryRowsInput, RowSet, StatementExecution

# Nothing is read back, so the request may use most of the attempt.
_NON_IDEMPOTENT_WRITE_WINDOW = 5 / 6
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


class _ReadBudgetExceeded(Exception):
    pass


class _CommitNotSent(Exception):
    pass


class _ReadBudgetTransport:
    def __init__(self, transport: Any, byte_limit: int) -> None:
        self.transport = transport
        self.byte_limit = byte_limit
        self.bytes_read = 0
        self.is_poisoned = False

    def recv_into(self, buffer: Any, length: int = 0) -> int:
        if self.is_poisoned:
            raise _ReadBudgetExceeded("SQL Server reply exceeded the read budget")
        received = int(self.transport.recv_into(buffer, length))
        self.bytes_read += received
        if self.bytes_read > self.byte_limit:
            self.is_poisoned = True
            raise _ReadBudgetExceeded("SQL Server reply exceeded the read budget")
        return received

    def set_control_budget(self) -> None:
        self.byte_limit = 4 << 20
        self.bytes_read = 0

    def __getattr__(self, name: str) -> Any:
        return getattr(self.transport, name)


class _CommitWriteTransport:
    def __init__(self, transport: Any) -> None:
        self.transport = transport
        self.bytes_sent = 0
        self.has_unobservable_write = False

    def sendall(self, data: Any, flags: int = 0) -> None:
        if not callable(getattr(self.transport, "send", None)):
            self.has_unobservable_write = True
            self.transport.sendall(data, flags)
            return
        data_view = memoryview(data)
        offset = 0
        while offset < len(data_view):
            sent = self.transport.send(data_view[offset:], flags)
            if sent <= 0:
                raise ConnectionError("SQL Server socket closed before COMMIT was sent")
            self.bytes_sent += sent
            offset += sent

    def __getattr__(self, name: str) -> Any:
        return getattr(self.transport, name)


class _AttemptDeadline:
    def __init__(self, seconds: float) -> None:
        self.expires_at = clock_time.monotonic() + seconds
        self.connection: Any = None
        self.sockets: list[socket.socket] = []
        self.lock = threading.Lock()
        self.expired = threading.Event()
        self.timer = threading.Timer(seconds, self._expire)
        self.timer.daemon = True
        self.timer.start()

    def _expire(self) -> None:
        with self.lock:
            self.expired.set()
            sockets = tuple(self.sockets)
            connection = self.connection
        for connected_socket in sockets:
            self._shutdown_socket(connected_socket)
        if connection is not None:
            self._shutdown_connection(connection)

    @staticmethod
    def _shutdown_socket(connected_socket: Any) -> None:
        try:
            connected_socket.shutdown(socket.SHUT_RDWR)
        except (AttributeError, OSError):
            pass

    def _shutdown_connection(self, connection: Any) -> None:
        tds_socket = getattr(connection, "_tds_socket", None)
        encrypted_socket = getattr(tds_socket, "sock", None)
        raw_socket = getattr(encrypted_socket, "_transport", encrypted_socket)
        if raw_socket is not None:
            self._shutdown_socket(raw_socket)

    def attach_socket(self, connected_socket: socket.socket) -> None:
        with self.lock:
            self.sockets.append(connected_socket)
            has_expired = self.expired.is_set()
        if has_expired:
            self._shutdown_socket(connected_socket)
            self.check()

    def attach_connection(self, connection: Any) -> None:
        with self.lock:
            self.connection = connection
            has_expired = self.expired.is_set()
        if has_expired:
            self._shutdown_connection(connection)
            self.check()

    def check(self) -> None:
        if self.expired.is_set():
            raise TimeoutError("SQL Server attempt exceeded its deadline")

    def remaining_seconds(self) -> float:
        self.check()
        remaining = self.expires_at - clock_time.monotonic()
        if remaining <= 0:
            raise TimeoutError("SQL Server attempt exceeded its deadline")
        return remaining

    def close(self) -> None:
        self.timer.cancel()


def _is_sql_letter(character: str) -> bool:
    return unicodedata.category(character).startswith("L")


def _is_sql_digit(character: str) -> bool:
    return unicodedata.category(character) == "Nd"


def _is_sql_identifier_character(character: str) -> bool:
    return _is_sql_letter(character) or _is_sql_digit(character) or character in "_#$@"


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
            while offset < len(statement) and _is_sql_identifier_character(statement[offset]):
                offset += 1
            tokens.append(("variable", statement[start:offset], start, offset))
        elif _is_sql_letter(character) or character in "_#$":
            offset += 1
            while offset < len(statement) and _is_sql_identifier_character(statement[offset]):
                offset += 1
            tokens.append(("word", statement[start:offset], start, offset))
        elif character == ";":
            offset += 1
            tokens.append(("semicolon", ";", start, offset))
        elif character == "(":
            offset += 1
            tokens.append(("open", "(", start, offset))
        elif _is_sql_digit(character):
            offset += 1
            while offset < len(statement) and _is_sql_identifier_character(statement[offset]):
                offset += 1
            tokens.append(("other", statement[start:offset], start, offset))
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
    first = next(
        (text.upper() if kind == "word" else "" for kind, text, _, _ in tokens if kind != "open"),
        "",
    )
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
        elif isinstance(parameter, int) and -(1 << 63) <= parameter < 1 << 63:
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
        if number in _RETRY_AVAILABILITY:
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
        if getattr(error, "severity", 0) >= 20:
            return "retry", FailureKind.AVAILABILITY, "SQL Server is temporarily unavailable", metadata
        return (
            "rejected",
            FailureKind.PROVIDER_REJECTION,
            f"SQL Server rejected request with error {number}",
            metadata,
        )
    if isinstance(error, _CommitNotSent):
        return "retry", FailureKind.TRANSPORT, "SQL Server COMMIT was not sent", {}
    if phase == "commit":
        return "uncertain", FailureKind.TRANSPORT, "SQL Server commit outcome is unknown", {}
    if isinstance(error, _InvalidResponse):
        return "invalid", FailureKind.PROTOCOL, str(error), {}
    error_text = str(error).lower()
    if phase == "connect" and (
        "you requested encryption but it is not supported by server" in error_text
        or "server returned unexpected encrypt_on value" in error_text
    ):
        return "rejected", FailureKind.PROVIDER_REJECTION, "SQL Server does not accept required TLS", {}
    if (
        isinstance(error, ssl.SSLError)
        or "certificate" in type(error).__name__.lower()
        or "certificate does not match host name" in error_text
        or "certificate verify failed" in error_text
    ):
        return "rejected", FailureKind.AUTHENTICATION, "SQL Server certificate was rejected", {}
    if isinstance(error, ValueError):
        return "defect", FailureKind.VALIDATION, str(error), {}
    if isinstance(error, tds_base.DataError):
        return "defect", FailureKind.VALIDATION, "SQL Server driver rejected a parameter value", {}
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
    if isinstance(error, tds_base.Error) and any(
        phrase in error_text
        for phrase in ("connection closed", "closed connection", "server closed connection")
    ):
        return "retry", FailureKind.TRANSPORT, "SQL Server connection failed before commit", {}
    if isinstance(
        error,
        (
            OSError,
            EOFError,
            ConnectionError,
            TimeoutError,
            tds_base.ClosedConnectionError,
            SSL.SysCallError,
            SSL.ZeroReturnError,
        ),
    ):
        return "retry", FailureKind.TRANSPORT, "SQL Server connection failed before commit", {}
    return "invalid", FailureKind.PROTOCOL, "SQL Server driver could not decode the reply", {}


def _receipt(call: Call, metadata: dict[str, str] | None = None, connection: Any = None) -> Receipt:
    receipt_metadata = dict(getattr(connection, "_dex_receipt_metadata", {}))
    receipt_metadata.update(metadata or {})
    return Receipt(
        CallID(str(call.id)),
        CONNECTOR_ID,
        datetime.now(UTC),
        IdempotencyKey(str(call.idempotency_key)),
        metadata=receipt_metadata,
    )


def _row_value(value: Any, type_name: str, scale: int | None = None) -> Any:
    if value is None:
        return None
    if type_name == "BIGINT" and isinstance(value, int):
        return str(value)
    if type_name in ("DECIMAL", "MONEY", "SMALLMONEY") and isinstance(value, (Decimal, str)):
        if isinstance(value, str):
            value = Decimal(value)
        declared_scale = 4 if type_name in ("MONEY", "SMALLMONEY") else scale
        if declared_scale is None or not 0 <= declared_scale <= 38:
            raise _InvalidResponse("SQL Server decimal scale is unavailable")
        with localcontext() as decimal_context:
            decimal_context.prec = 50
            return format(value.quantize(Decimal(1).scaleb(-declared_scale)), "f")
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
    if isinstance(value, bytes) and type_name in (
        "BINARY",
        "VARBINARY",
        "IMAGE",
        "GEOGRAPHY",
        "GEOMETRY",
        "HIERARCHYID",
    ):
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
        if field[1] == 240:
            native_columns = getattr(
                getattr(getattr(cursor, "_session", None), "res_info", None), "columns", ()
            )
            serializer = (
                getattr(native_columns[len(columns)], "serializer", None)
                if len(native_columns) > len(columns)
                else None
            )
            type_name = str(getattr(serializer, "type_name", "UNKNOWN")).upper()
            if type_name not in ("GEOGRAPHY", "GEOMETRY", "HIERARCHYID"):
                raise _InvalidResponse(f'column "{name}" has an unsupported SQL CLR type')
        elif type_name == "UNKNOWN":
            raise _InvalidResponse(f'column "{name}" has an unsupported SQL type')
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
    reader = getattr(getattr(cursor, "_session", None), "_reader", None)
    budget_transport = None
    if reader is not None:
        budget_transport = _ReadBudgetTransport(reader._transport, max_bytes + (64 << 10))
        reader._transport = budget_transport
    encoded_bytes = 2
    try:
        while True:
            raw = cursor.fetchone()
            if raw is None:
                while callable(getattr(cursor, "nextset", None)) and cursor.nextset():
                    if cursor.description:
                        raise ValueError("statement returned more than one result set")
                return tuple(columns), tuple(rows), False
            if len(rows) >= max_rows:
                return tuple(columns), tuple(rows), True
            row = {
                column.name: _row_value(
                    value,
                    column.type_name,
                    description[index][5]
                    if len(description[index]) > 5 and isinstance(description[index][5], int)
                    else None,
                )
                for index, (column, value) in enumerate(zip(columns, raw, strict=True))
            }
            encoded_row = json.dumps(row, separators=(",", ":"), sort_keys=True, ensure_ascii=False)
            for character, escaped in (
                ("<", "\\u003c"),
                (">", "\\u003e"),
                ("&", "\\u0026"),
                ("\u2028", "\\u2028"),
                ("\u2029", "\\u2029"),
            ):
                encoded_row = encoded_row.replace(character, escaped)
            encoded_bytes += len(encoded_row.encode("utf-8")) + 1
            if encoded_bytes > max_bytes:
                return tuple(columns), tuple(rows), True
            rows.append(row)
    except _ReadBudgetExceeded:
        return tuple(columns), tuple(rows), True
    finally:
        if budget_transport is not None:
            budget_transport.set_control_budget()


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
        password = await _resolve_password(self.client, call)
        if isinstance(password, _WriteOutcome):
            credential_failure = Failure(password.kind, CONNECTOR_ID, QUERY_OPERATION_ID, password.message)
            if password.disposition == "retry":
                return QueryAttempt.retry(credential_failure)
            return QueryAttempt.branch_result(DEFECT, RowSet(), Receipt(), credential_failure)
        return await asyncio.to_thread(self._invoke_sync, call, statement, parameters, password)

    def _invoke_sync(
        self, call: Call, statement: str, parameters: dict[str, Any], password: str
    ) -> QueryAttempt[RowSet]:
        phase = "connect"
        connection = None
        statement_deadline: _AttemptDeadline | None = None
        deadline = _AttemptDeadline(
            self.client.config.connect_timeout_seconds + self.client.config.statement_timeout_seconds + 5
        )
        try:
            connection = self.client.open_connection(password, deadline)
            deadline.check()
            phase = "statement"
            statement_deadline = _AttemptDeadline(self.client.config.statement_timeout_seconds)
            statement_deadline.attach_connection(connection)
            cursor = connection.cursor()
            cursor.execute(statement, parameters)
            deadline.check()
            statement_deadline.check()
            columns, rows, truncated = _read_rows(
                cursor, self.client.config.max_rows, self.client.config.max_response_bytes
            )
            deadline.check()
            statement_deadline.check()
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
                return QueryAttempt.branch_result(
                    TRUNCATED, value, _receipt(call, connection=connection), failure
                )
            return QueryAttempt.branch_result(COMPLETED, value, _receipt(call, connection=connection))
        except Exception as error:
            has_timed_out = deadline.expired.is_set() or (
                statement_deadline is not None and statement_deadline.expired.is_set()
            )
            classified_error = TimeoutError("SQL Server statement timed out") if has_timed_out else error
            disposition, kind, message, metadata = _classify(classified_error, phase)
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
            return QueryAttempt.branch_result(branch, RowSet(), _receipt(call, metadata, connection), failure)
        finally:
            if statement_deadline is not None:
                statement_deadline.close()
            if connection is not None:
                try:
                    connection.close()
                except Exception:
                    # The read outcome was already classified and the session cannot be reused.
                    pass
            deadline.close()


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
        """Run one repeat-safe write; an unknown COMMIT outcome returns Retry, never uncertain."""
        try:
            statement, parameters = _bind_execute(call, input)
        except ValueError as error:
            return _write_defect(EXECUTE_OPERATION_ID, FailureKind.VALIDATION, str(error))
        password = await _resolve_password(self.client, call)
        if isinstance(password, _WriteOutcome):
            return _write_attempt(EXECUTE_OPERATION_ID, password)
        outcome = await asyncio.to_thread(
            _StatementWriter(self.client).run, call, input, statement, parameters, password, _commit_directly
        )
        if outcome.disposition == "uncertain":
            # The statement is repeat-safe, so an unknown COMMIT outcome runs it again.
            outcome = replace(outcome, disposition="retry")
        return _write_attempt(EXECUTE_OPERATION_ID, outcome)


class ExecuteStatementNonIdempotentOperation:
    """Run one bounded write that must not run twice, committing it at most once."""

    def __init__(self, client: Any) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest Mutation identity and branch contract."""
        return EXECUTE_NON_IDEMPOTENT_DEFINITION

    def idempotency_key(self, call_id: CallID, _input: ExecuteStatementInput) -> IdempotencyKey:
        """Use the stable Dex Call ID for an optional final SQL placeholder."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: ExecuteStatementInput) -> MutationAttempt[StatementExecution]:
        """Commit at most once behind a single-dispatch fence; an unknown COMMIT selects uncertain.

        A later attempt after a possibly sent COMMIT, or after a lost Worker, selects uncertain
        without connecting. A proven rollback before or at COMMIT returns Retry, and an
        unreadable checkpoint returns Retry without connecting.
        """
        try:
            statement, parameters = _bind_execute(call, input)
        except ValueError as error:
            return _write_defect(EXECUTE_NON_IDEMPOTENT_OPERATION_ID, FailureKind.VALIDATION, str(error))
        try:
            fence = Fence(
                call,
                EXECUTE_NON_IDEMPOTENT_DEFINITION.step_defaults.execute_method_timeout,
                write_window_fraction=_NON_IDEMPOTENT_WRITE_WINDOW,
                max_sends=call.max_sends,
            )
        except ValueError:
            return _write_defect(
                EXECUTE_NON_IDEMPOTENT_OPERATION_ID,
                FailureKind.LOCAL_DEFECT,
                "SQL Server dispatch fence could not be constructed",
            )
        decision = await fence.decide()
        if decision.kind == DecisionKind.FAIL_CLOSED:
            return MutationAttempt.retry(
                Failure(
                    FailureKind.AVAILABILITY,
                    CONNECTOR_ID,
                    EXECUTE_NON_IDEMPOTENT_OPERATION_ID,
                    "the dispatch checkpoint could not be read, so nothing was sent in this attempt",
                )
            )
        if decision.kind != DecisionKind.SEND_ALLOWED:
            return MutationAttempt.uncertain(
                StatementExecution(),
                Failure(
                    FailureKind.TRANSPORT,
                    CONNECTOR_ID,
                    EXECUTE_NON_IDEMPOTENT_OPERATION_ID,
                    "an earlier attempt may have committed the statement, and executeNonIdempotent "
                    "never runs it again",
                ),
                _receipt(call),
            )
        # Overcounts when an earlier attempt proved its COMMIT unsent, which only costs a rejection branch.
        has_possible_earlier_send = decision.prior_send_upper_bound > 0
        password = await _resolve_password(self.client, call)
        if isinstance(password, _WriteOutcome):
            outcome = password
        else:
            loop = asyncio.get_running_loop()

            def commit_once(commit: Callable[[], None]) -> None:
                async def send(_write_deadline: datetime) -> None:
                    await asyncio.to_thread(commit)

                try:
                    asyncio.run_coroutine_threadsafe(fence.dispatch(send), loop).result()
                except (CheckpointError, DispatchNotAllowedError, InsufficientAttemptTimeError) as error:
                    raise _CommitNotSent() from error

            outcome = await asyncio.to_thread(
                _StatementWriter(self.client).run, call, input, statement, parameters, password, commit_once
            )
        if outcome.disposition is None:
            return _write_attempt(EXECUTE_NON_IDEMPOTENT_OPERATION_ID, outcome)
        if not outcome.is_commit_phase and has_possible_earlier_send:
            # notSent would hide the earlier possible COMMIT; without it the next attempt selects uncertain.
            if outcome.disposition != "retry":
                outcome = replace(outcome, disposition="uncertain")
            return _write_attempt(EXECUTE_NON_IDEMPOTENT_OPERATION_ID, outcome)
        try:
            if not outcome.is_commit_phase:
                await fence.record_not_sent()
            elif outcome.disposition != "uncertain":
                await fence.record_can_resend()
        except Exception:
            # A lost checkpoint only makes a later attempt select uncertain, never send twice.
            pass
        if outcome.disposition not in ("retry", "uncertain") and has_possible_earlier_send:
            outcome = replace(outcome, disposition="uncertain")
        return _write_attempt(EXECUTE_NON_IDEMPOTENT_OPERATION_ID, outcome)


@dataclass(frozen=True)
class _WriteOutcome:
    """A committed write when disposition is None, otherwise a failure and whether it happened at COMMIT."""

    disposition: str | None
    kind: FailureKind = FailureKind.PROTOCOL
    message: str = ""
    receipt: Receipt | None = None
    value: StatementExecution | None = None
    is_commit_phase: bool = False


def _commit_directly(commit: Callable[[], None]) -> None:
    commit()


def _bind_execute(call: Call, input: ExecuteStatementInput) -> tuple[str, dict[str, Any]]:
    if input.max_rows_affected is not None and input.max_rows_affected < 0:
        raise ValueError("maxRowsAffected cannot be negative")
    if input.idempotency_key_placeholder not in (0, len(input.parameters) + 1):
        raise ValueError("idempotencyKeyPlaceholder must be the last placeholder")
    key = str(call.idempotency_key) if input.idempotency_key_placeholder else None
    return _bind_statement(input.statement, input.parameters, True, key)


async def _resolve_password(client: Any, call: Call) -> str | _WriteOutcome:
    """Return the password, or the failure to report before connecting.

    A terminal credential failure or an empty password selects defect; any other failure returns Retry.
    """
    try:
        credentials = await client.credentials.resolve(call)
    except Exception as error:
        if not _is_terminal_credential_error(error):
            return _WriteOutcome(
                "retry",
                FailureKind.AVAILABILITY,
                "SQL Server connection credentials are temporarily unavailable, so nothing was sent",
            )
        password = ""
    else:
        password = credentials.password.reveal()
    if not password:
        return _WriteOutcome(
            "defect", FailureKind.AUTHENTICATION, "SQL Server connection credentials are unavailable"
        )
    return str(password)


class _CredentialMaterialError(ValueError):
    """Stored credential material fails the connector credential checks, so no retry can succeed."""


def _is_terminal_credential_error(error: Exception) -> bool:
    """Report a credential failure that no later attempt resolves without a person."""
    return isinstance(
        error,
        (
            _CredentialMaterialError,
            ReauthorizationRequiredError,
            ObjectNotFoundError,
            ConnectionNotSelectedError,
            CredentialBrokerRejectedError,
        ),
    )


def _write_attempt(operation: str, outcome: _WriteOutcome) -> MutationAttempt[StatementExecution]:
    """Map an outcome to an attempt; both write operations share their branch IDs."""
    receipt = outcome.receipt or Receipt()
    if outcome.disposition is None:
        return MutationAttempt.branch_result(COMPLETED, outcome.value or StatementExecution(), receipt)
    failure = Failure(outcome.kind, CONNECTOR_ID, operation, outcome.message)
    if outcome.disposition == "retry":
        return MutationAttempt.retry(failure)
    if outcome.disposition == "uncertain":
        return MutationAttempt.uncertain(StatementExecution(), failure, receipt)
    branch = _WRITE_BRANCHES.get(outcome.disposition, PROVIDER_REJECTED)
    return MutationAttempt.branch_result(branch, StatementExecution(), receipt, failure)


def _write_defect(operation: str, kind: FailureKind, message: str) -> MutationAttempt[StatementExecution]:
    return _write_attempt(operation, _WriteOutcome("defect", kind, message))


_WRITE_BRANCHES = {"limitExceeded": LIMIT_EXCEEDED, "invalid": INVALID_RESPONSE, "defect": DEFECT}


class _StatementWriter:
    """Run one write transaction on a worker thread for either write operation."""

    def __init__(self, client: Any) -> None:
        self.client = client

    def run(
        self,
        call: Call,
        input: ExecuteStatementInput,
        statement: str,
        parameters: dict[str, Any],
        password: str,
        commit_gate: Callable[[Callable[[], None]], None],
    ) -> _WriteOutcome:
        """Run one statement in its own transaction and pass its COMMIT to `commit_gate`.

        Every failure before `commit_gate` runs the COMMIT leaves nothing committed.
        """
        phase = "connect"
        connection = None
        statement_deadline: _AttemptDeadline | None = None
        deadline = _AttemptDeadline(
            self.client.config.connect_timeout_seconds + self.client.config.statement_timeout_seconds + 5
        )
        try:
            connection = self.client.open_connection(password, deadline)
            deadline.check()
            phase = "statement"
            statement_deadline = _AttemptDeadline(self.client.config.statement_timeout_seconds)
            statement_deadline.attach_connection(connection)
            cursor = connection.cursor()
            done_counts: list[int] = []
            session = getattr(cursor, "_session", None)
            if session is not None:
                process_end = session.process_end

                def capture_done_count(marker: int) -> None:
                    process_end(marker)
                    if session.done_flags & tds_base.TDS_DONE_COUNT:
                        done_counts.append(int(session.rows_affected))

                session.process_end = capture_done_count
            cursor.execute(statement, parameters)
            deadline.check()
            statement_deadline.check()
            if input.returns_output_rows:
                columns, rows, truncated = _read_rows(
                    cursor, self.client.config.max_rows, self.client.config.max_response_bytes
                )
                if truncated:
                    return _WriteOutcome(
                        "limitExceeded",
                        FailureKind.RESPONSE_TOO_LARGE,
                        "SQL Server OUTPUT rows exceeded bounds",
                        _receipt(call, connection=connection),
                    )
                if not columns:
                    raise ValueError("returnsOutputRows is set but no OUTPUT result set was returned")
                value = StatementExecution(len(rows), columns, rows)
            else:
                fallback_counts = [int(cursor.rowcount)] if session is None and cursor.rowcount >= 0 else []
                reader = getattr(session, "_reader", None)
                budget_transport = None
                if reader is not None:
                    budget_transport = _ReadBudgetTransport(
                        reader._transport, self.client.config.max_response_bytes + (64 << 10)
                    )
                    reader._transport = budget_transport
                try:
                    while cursor.nextset():
                        if session is None and cursor.rowcount >= 0:
                            fallback_counts.append(int(cursor.rowcount))
                except _ReadBudgetExceeded:
                    return _WriteOutcome(
                        "limitExceeded",
                        FailureKind.RESPONSE_TOO_LARGE,
                        "SQL Server statement reply exceeded the read budget",
                        _receipt(call, connection=connection),
                    )
                finally:
                    if budget_transport is not None:
                        budget_transport.set_control_budget()
                counts = done_counts if session is not None else fallback_counts
                rows_affected = sum(counts)
                has_count = bool(counts)
                if not has_count and input.max_rows_affected is not None:
                    raise _InvalidResponse("SQL Server did not report rows affected")
                value = StatementExecution(rows_affected)
            if input.max_rows_affected is not None and value.rows_affected > input.max_rows_affected:
                return _WriteOutcome(
                    "limitExceeded",
                    FailureKind.CONFLICT,
                    "SQL Server rows affected exceeded maxRowsAffected",
                    _receipt(call, connection=connection),
                )
            statement_deadline.check()
            statement_deadline.close()
            try:
                deadline.check()
            except TimeoutError as error:
                raise _CommitNotSent() from error
            phase = "commit"
            commit_gate(lambda: _commit_counting_sent_bytes(connection))
            return _WriteOutcome(None, receipt=_receipt(call, connection=connection), value=value)
        except Exception as error:
            has_timed_out = deadline.expired.is_set() or (
                statement_deadline is not None and statement_deadline.expired.is_set()
            )
            classified_error = (
                TimeoutError("SQL Server statement timed out")
                if has_timed_out and not isinstance(error, _CommitNotSent) and phase != "commit"
                else error
            )
            disposition, kind, message, metadata = _classify(classified_error, phase)
            return _WriteOutcome(
                disposition,
                kind,
                message,
                _receipt(call, metadata, connection),
                is_commit_phase=phase == "commit",
            )
        finally:
            if statement_deadline is not None:
                statement_deadline.close()
            if connection is not None:
                try:
                    connection.close()
                except Exception:
                    # Outcome was already classified and the native connection is unusable.
                    pass
            deadline.close()


def _commit_counting_sent_bytes(connection: Any) -> None:
    """Send COMMIT; a failure before any COMMIT byte left the client raises _CommitNotSent."""
    commit_session = getattr(getattr(connection, "_tds_socket", None), "main_session", None)
    writer = getattr(commit_session, "_writer", None)
    commit_transport = None
    transport_owner = None
    if writer is not None:
        writer_transport = writer._transport
        transport_owner = writer_transport if hasattr(writer_transport, "_transport") else writer
        commit_transport = _CommitWriteTransport(transport_owner._transport)
        transport_owner._transport = commit_transport
    try:
        connection.commit()
    except Exception as error:
        if (
            commit_transport is not None
            and commit_transport.bytes_sent == 0
            and not commit_transport.has_unobservable_write
        ):
            raise _CommitNotSent() from error
        raise
    finally:
        if commit_transport is not None and transport_owner is not None:
            transport_owner._transport = commit_transport.transport
