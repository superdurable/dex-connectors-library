# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Native MySQL client and the two Dex operation invocations."""

import asyncio
import base64
import json
import math
import os
import re
import socket
import ssl
import struct
from dataclasses import dataclass, replace
from datetime import UTC, date, datetime, time, timedelta
from decimal import ROUND_HALF_EVEN, Decimal, localcontext
from typing import Any

import httpx
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
    Receipt,
)

from mysql.connector import Error as MySQLError
from mysql.connector.connection import MySQLConnection
from mysql.connector.constants import ClientFlag, FieldFlag, FieldType
from mysql.connector.protocol import MySQLProtocol
from mysql.connector.utils import read_lc_string

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import (
    COMPLETED,
    DEFECT,
    EXECUTE_DEFINITION,
    LIMIT_EXCEEDED,
    PROVIDER_REJECTED,
    QUERY_DEFINITION,
    TRUNCATED,
)
from .models import Column, ExecuteStatementInput, QueryRowsInput, RowSet, StatementExecution

_READ_WORDS = frozenset({"SELECT", "WITH", "TABLE", "VALUES", "SHOW", "EXPLAIN", "DESCRIBE", "DESC"})
_WRITE_WORDS = frozenset({"INSERT", "UPDATE", "DELETE", "REPLACE", "WITH"})
_SQL_MODE = (
    "ONLY_FULL_GROUP_BY,STRICT_TRANS_TABLES,NO_ZERO_IN_DATE,NO_ZERO_DATE,"
    "ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION"
)
_RETRY_CONFLICT = frozenset({1205, 1213})
_RETRY_AVAILABILITY = frozenset(
    {1021, 1037, 1040, 1041, 1053, 1158, 1159, 1160, 1161, 1203, 1615, 1637, 1927, 4031}
)
_AUTHENTICATION = frozenset({1045, 1129, 1130, 1251, 1698, 1820, 1862, 3118, 3159, 4151})
_AUTHORIZATION = frozenset({1044, 1142, 1143, 1227, 1290, 1370, 1792, 1836})
_NOT_FOUND = frozenset({1049, 1054, 1109, 1146, 1305})
_CONFLICT = frozenset({3819, 4025})
_TIMEOUT = frozenset({1317, 1969, 3024})
_VALIDATION = frozenset({1235, 1295, 1366})
_TYPE_NAMES = {
    FieldType.TINY: "TINYINT",
    FieldType.SHORT: "SMALLINT",
    FieldType.INT24: "MEDIUMINT",
    FieldType.LONG: "INT",
    FieldType.LONGLONG: "BIGINT",
    FieldType.YEAR: "YEAR",
    FieldType.FLOAT: "FLOAT",
    FieldType.DOUBLE: "DOUBLE",
    FieldType.DECIMAL: "DECIMAL",
    FieldType.NEWDECIMAL: "DECIMAL",
    FieldType.DATE: "DATE",
    FieldType.DATETIME: "DATETIME",
    FieldType.TIMESTAMP: "TIMESTAMP",
    FieldType.TIME: "TIME",
    FieldType.BIT: "BIT",
    FieldType.JSON: "JSON",
    FieldType.NULL: "NULL",
    FieldType.ENUM: "ENUM",
    FieldType.SET: "SET",
    FieldType.VARCHAR: "VARCHAR",
    FieldType.VAR_STRING: "VARCHAR",
    FieldType.STRING: "CHAR",
    FieldType.TINY_BLOB: "TINYBLOB",
    FieldType.BLOB: "BLOB",
    FieldType.MEDIUM_BLOB: "MEDIUMBLOB",
    FieldType.LONG_BLOB: "LONGBLOB",
    FieldType.GEOMETRY: "GEOMETRY",
    FieldType.VECTOR: "VECTOR",
}


@dataclass(frozen=True)
class _Disposition:
    branch: str
    kind: FailureKind
    message: str
    number: int = 0
    sql_state: str = ""


class _TLSUnavailable(Exception):
    pass


class _CommitNotSent(Exception):
    pass


class _DuplicateColumn(Exception):
    pass


class _UndecodableColumn(Exception):
    pass


class _InvalidTrustedRoots(Exception):
    pass


class _ParameterCountMismatch(Exception):
    pass


class _SendMeter:
    def __init__(self, socket: Any) -> None:
        self.socket = socket
        self.written_bytes = 0

    def sendall(self, payload: bytes) -> None:
        remaining = memoryview(payload)
        while remaining:
            written = self.socket.send(remaining)
            if written == 0:
                raise ConnectionError("socket closed before COMMIT was sent")
            self.written_bytes += written
            remaining = remaining[written:]

    def __getattr__(self, name: str) -> Any:
        return getattr(self.socket, name)


class _ExactValueProtocol(MySQLProtocol):
    @staticmethod
    def parse_column(packet: bytes, encoding: str = "utf-8") -> Any:
        field = MySQLProtocol.parse_column(packet, encoding)
        remaining = packet[4:]
        for _ in range(6):
            remaining, _ = read_lc_string(remaining)
        decimals = struct.unpack("<xHIBHBxx", remaining)[4]
        return (*field[:5], decimals, *field[6:])

    @staticmethod
    def _parse_binary_timestamp(packet: bytes, field_type: int) -> tuple[bytes, Any]:
        length = packet[0]
        if length not in {0, 4, 7, 11}:
            raise ValueError("invalid binary date or timestamp")
        if length == 0:
            text = "0000-00-00"
            if field_type != FieldType.DATE:
                text += " 00:00:00"
            return packet[1:], text
        year = struct.unpack_from("<H", packet, 1)[0]
        month, day = packet[3], packet[4]
        text = f"{year:04}-{month:02}-{day:02}"
        if field_type != FieldType.DATE:
            hour, minute, second = (packet[5], packet[6], packet[7]) if length >= 7 else (0, 0, 0)
            text += f" {hour:02}:{minute:02}:{second:02}"
            if length == 11:
                text += f".{struct.unpack_from('<I', packet, 8)[0]:06}"
        try:
            return MySQLProtocol._parse_binary_timestamp(packet, field_type)
        except ValueError:
            return packet[length + 1 :], text

    @staticmethod
    def _parse_binary_time(packet: bytes) -> tuple[bytes, timedelta]:
        length = packet[0]
        if length == 0:
            return packet[1:], timedelta()
        if length not in {8, 12} or packet[1] not in {0, 1}:
            raise ValueError("invalid binary TIME")
        days = struct.unpack_from("<I", packet, 2)[0]
        microseconds = struct.unpack_from("<I", packet, 9)[0] if length == 12 else 0
        duration = timedelta(
            days=days, hours=packet[6], minutes=packet[7], seconds=packet[8], microseconds=microseconds
        )
        return packet[length + 1 :], -duration if packet[1] else duration

    def _parse_binary_values(self, fields: Any, packet: bytes, charset: str = "utf-8") -> tuple[Any, ...]:
        # The driver treats YEAR's two-byte integer as a length-encoded string.
        integer_year_fields = [
            (field[0], FieldType.SHORT, *field[2:]) if field[1] == FieldType.YEAR else field
            for field in fields
        ]
        return super()._parse_binary_values(integer_year_fields, packet, charset)


class _ExactValueConnection(MySQLConnection):
    def _open_connection(self) -> None:
        super()._open_connection()
        self._protocol = _ExactValueProtocol()


class _StrictTLSConnection(_ExactValueConnection):
    def _do_handshake(self) -> None:
        try:
            super()._do_handshake()
        except MySQLError as error:
            capabilities = self._handshake.get("capabilities") if self._handshake is not None else None
            if error.errno == 2026 and isinstance(capabilities, int) and not capabilities & ClientFlag.SSL:
                raise _TLSUnavailable("the MySQL server does not accept TLS") from error
            raise
        capabilities = self._handshake.get("capabilities") if self._handshake is not None else None
        if not isinstance(capabilities, int) or not capabilities & ClientFlag.SSL:
            raise _TLSUnavailable("the MySQL server does not accept TLS")


class Client:
    """Create a fresh native driver connection for every MySQL operation."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("MySQL config and credential source are required")
        config = replace(
            config,
            port=config.port or 3306,
            ssl_mode=config.ssl_mode or "required",
            connect_timeout_seconds=config.connect_timeout_seconds or 5,
            statement_timeout_seconds=config.statement_timeout_seconds or 5,
            max_rows=config.max_rows or 1000,
            max_response_bytes=config.max_response_bytes or 1 << 20,
        )
        host = config.host
        if (
            not host
            or host.strip() != host
            or "://" in host
            or "," in host
            or "\x00" in host
            or "'" in host
            or (":" in host and not _is_ipv6(host))
            or host.startswith("[")
        ):
            raise ValueError("MySQL host must identify one server without a scheme or port")
        if not 1 <= config.port <= 65535 or not config.database.strip() or not config.user.strip():
            raise ValueError("MySQL port, database, or user is invalid")
        if any(ord(char) < 32 for char in config.database + config.user):
            raise ValueError("MySQL database or user contains a control character")
        if config.ssl_mode not in {"required", "verify-ca", "verify-identity", "disabled"}:
            raise ValueError("MySQL sslMode is invalid")
        is_socket = host.startswith("/")
        if config.ssl_mode == "disabled" and host not in {"localhost", "127.0.0.1", "::1"} and not is_socket:
            raise ValueError("MySQL sslMode disabled requires a local host")
        if is_socket and config.ssl_mode != "disabled":
            raise ValueError("MySQL Unix sockets require sslMode disabled")
        if (
            config.connect_timeout_seconds <= 0
            or config.statement_timeout_seconds < 0.001
            or not 1 <= config.max_rows <= 100000
            or not 1 <= config.max_response_bytes <= 16 << 20
        ):
            raise ValueError("MySQL timeouts or response limits are invalid")
        self.config = config
        self.credentials = credentials
        # Generated project connections pass the HTTP argument used by other connectors.
        self._unused_http_client = http_client

    def query(self) -> "QueryRowsOperation":
        """Bind the read-only query operation."""
        return QueryRowsOperation(self)

    def execute(self) -> "ExecuteStatementOperation":
        """Bind the transactional write operation."""
        return ExecuteStatementOperation(self)

    async def aclose(self) -> None:
        """Release no resources: each invocation closes its own connection."""

    def _open(self, password: str) -> Any:
        config = self.config
        options: dict[str, Any] = {
            "user": config.user,
            "password": password,
            "database": config.database,
            "connection_timeout": config.connect_timeout_seconds,
            "read_timeout": math.ceil(config.statement_timeout_seconds + 5),
            "write_timeout": math.ceil(config.statement_timeout_seconds + 5),
            "autocommit": True,
            "use_pure": True,
            "allow_local_infile": False,
            "consume_results": False,
        }
        if config.host.startswith("/"):
            options["unix_socket"] = config.host
        else:
            options.update(host=config.host, port=config.port)
        if config.ssl_mode == "disabled":
            options["ssl_disabled"] = True
        else:
            options["ssl_disabled"] = False
            options["ssl_verify_cert"] = config.ssl_mode in {"verify-ca", "verify-identity"}
            options["ssl_verify_identity"] = config.ssl_mode == "verify-identity"
            options["tls_versions"] = ["TLSv1.2", "TLSv1.3"]
            trusted_roots = os.environ.get("MYSQL_SSL_CA")
            if trusted_roots and options["ssl_verify_cert"]:
                try:
                    ssl.create_default_context(cafile=trusted_roots)
                except (OSError, ssl.SSLError) as error:
                    raise _InvalidTrustedRoots() from error
                options["ssl_ca"] = trusted_roots
            return _StrictTLSConnection(**options)
        return _ExactValueConnection(**options)

    def _session(self, connection: Any) -> tuple[str, str]:
        cursor = connection.cursor()
        try:
            cursor.execute("SELECT CONNECTION_ID(), VERSION()")
            identity = cursor.fetchone()
            if identity is None or len(identity) != 2 or not str(identity[0]).isdigit():
                raise ValueError("server returned an unreadable CONNECTION_ID() or VERSION()")
            connection_id, version = str(identity[0]), str(identity[1])
            timeout = self.config.statement_timeout_seconds
            settings = [
                "time_zone = '+00:00'",
                f"sql_mode = '{_SQL_MODE}'",
                "wait_timeout = 30",
                f"innodb_lock_wait_timeout = {math.ceil(timeout)}",
                f"lock_wait_timeout = {math.ceil(timeout)}",
            ]
            if "mariadb" in version.lower():
                settings.append(f"max_statement_time = {timeout:.6f}")
            else:
                settings.append(f"max_execution_time = {math.ceil(timeout * 1000)}")
            cursor.execute("SET SESSION " + ", ".join(settings))
            return connection_id, version
        finally:
            cursor.close()


class QueryRowsOperation:
    """Run one prepared statement in a read-only transaction."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest's query identity and branches."""
        return QUERY_DEFINITION

    async def invoke(self, call: Call, input: QueryRowsInput) -> QueryAttempt[RowSet]:
        """Classify one bounded read attempt."""
        try:
            parameters = _validate_statement(input.statement, input.parameters, _READ_WORDS)
        except ValueError as error:
            return _query_branch(DEFECT, FailureKind.VALIDATION, str(error))
        try:
            password = (await self.client.credentials.resolve(call)).password.reveal()
            if not password:
                raise ValueError("empty password")
        except (ValueError, LookupError, OSError):
            return _query_branch(
                DEFECT, FailureKind.AUTHENTICATION, "MySQL connection credentials are unavailable"
            )
        return await asyncio.to_thread(self._invoke_sync, call, input, parameters, password)

    def _invoke_sync(
        self, call: Call, input: QueryRowsInput, parameters: tuple[Any, ...], password: str
    ) -> QueryAttempt[RowSet]:
        connection = None
        metadata: dict[str, str] = {}
        phase = "connect"
        try:
            connection = self.client._open(password)
            phase = "begin"
            identity, version = self.client._session(connection)
            metadata.update(connectionId=identity, serverVersion=version)
            cursor = connection.cursor()
            cursor.execute("START TRANSACTION READ ONLY")
            cursor.close()
            phase = "statement"
            prepared_cursor = connection.cursor(prepared=True)
            _execute_prepared(prepared_cursor, input.statement, parameters)
            columns, rows, truncated = _read_rows(prepared_cursor, self.client.config)
            prepared_cursor.close()
            receipt = _receipt(call, metadata)
            if truncated:
                failure = _failure(
                    "query",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "the statement returned more rows or bytes than maxRows or maxResponseBytes allow",
                )
                return QueryAttempt.branch_result(TRUNCATED, RowSet(columns, rows, True), receipt, failure)
            return QueryAttempt.branch_result(COMPLETED, RowSet(columns, rows), receipt)
        except Exception as error:
            disposition = _classify(error, phase)
            metadata.update(_error_metadata(disposition))
            if disposition.branch == "retry":
                return QueryAttempt.retry(_failure("query", disposition.kind, disposition.message))
            branch = (
                "invalidResponse"
                if disposition.branch == "invalidResponse"
                else "defect"
                if disposition.branch == "defect"
                else PROVIDER_REJECTED
            )
            return _query_branch(branch, disposition.kind, disposition.message, _receipt(call, metadata))
        finally:
            if connection is not None:
                try:
                    connection.close()
                except (MySQLError, OSError):
                    # A failed close cannot change an already classified operation result.
                    pass


class ExecuteStatementOperation:
    """Run one prepared write and commit only after all limits pass."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest's mutation identity and branches."""
        return EXECUTE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: ExecuteStatementInput) -> IdempotencyKey:
        """Keep the Call ID stable across retries of one Step execution."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: ExecuteStatementInput) -> MutationAttempt[StatementExecution]:
        """Classify one write, including a possibly received COMMIT."""
        try:
            parameters = _validate_statement(input.statement, input.parameters, _WRITE_WORDS)
            if input.max_rows_affected is not None and input.max_rows_affected < 0:
                raise ValueError("maxRowsAffected cannot be negative")
            if input.idempotency_key_placeholder not in {0, len(parameters) + 1}:
                raise ValueError("idempotencyKeyPlaceholder must be the last placeholder")
            if input.idempotency_key_placeholder:
                parameters += (str(call.idempotency_key),)
        except ValueError as error:
            return _execute_branch(DEFECT, FailureKind.VALIDATION, str(error))
        try:
            password = (await self.client.credentials.resolve(call)).password.reveal()
            if not password:
                raise ValueError("empty password")
        except (ValueError, LookupError, OSError):
            return _execute_branch(
                DEFECT, FailureKind.AUTHENTICATION, "MySQL connection credentials are unavailable"
            )
        return await asyncio.to_thread(self._invoke_sync, call, input, parameters, password)

    def _invoke_sync(
        self, call: Call, input: ExecuteStatementInput, parameters: tuple[Any, ...], password: str
    ) -> MutationAttempt[StatementExecution]:
        connection = None
        metadata: dict[str, str] = {}
        phase = "connect"
        try:
            connection = self.client._open(password)
            phase = "begin"
            identity, version = self.client._session(connection)
            metadata.update(connectionId=identity, serverVersion=version)
            connection.start_transaction()
            phase = "statement"
            prepared_cursor = connection.cursor(prepared=True)
            _execute_prepared(prepared_cursor, input.statement, parameters)
            columns, rows, truncated = _read_rows(prepared_cursor, self.client.config)
            if truncated:
                return _execute_branch(
                    LIMIT_EXCEEDED,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "RETURNING rows exceeded maxRows or maxResponseBytes, so the transaction was rolled back",
                    _receipt(call, metadata),
                )
            cursor = connection.cursor()
            cursor.execute("SELECT ROW_COUNT(), LAST_INSERT_ID(), @@warning_count")
            outcome = cursor.fetchone()
            cursor.close()
            prepared_cursor.close()
            if outcome is None or len(outcome) != 3:
                raise ValueError(
                    "server returned an unreadable ROW_COUNT(), LAST_INSERT_ID(), or @@warning_count"
                )
            affected = int(outcome[0])
            if affected < 0:
                affected = len(rows)
            if input.max_rows_affected is not None and affected > input.max_rows_affected:
                return _execute_branch(
                    LIMIT_EXCEEDED,
                    FailureKind.CONFLICT,
                    (
                        f"the statement affected {affected} rows but maxRowsAffected is "
                        f"{input.max_rows_affected}, so the transaction was rolled back"
                    ),
                    _receipt(call, metadata),
                )
            phase = "commit"
            _commit_transaction(connection)
            result = StatementExecution(affected, str(outcome[1]), int(outcome[2]), columns, rows)
            return MutationAttempt.branch_result(COMPLETED, result, _receipt(call, metadata))
        except Exception as error:
            disposition = _classify(error, phase)
            metadata.update(_error_metadata(disposition))
            failure = _failure("execute", disposition.kind, disposition.message)
            if disposition.branch == "retry":
                return MutationAttempt.retry(failure)
            if disposition.branch == "uncertain":
                return MutationAttempt.uncertain(_empty_execution(), failure, _receipt(call, metadata))
            branch = (
                "invalidResponse"
                if disposition.branch == "invalidResponse"
                else "defect"
                if disposition.branch == "defect"
                else PROVIDER_REJECTED
            )
            return MutationAttempt.branch_result(
                branch, _empty_execution(), _receipt(call, metadata), failure
            )
        finally:
            if connection is not None:
                try:
                    connection.close()
                except (MySQLError, OSError):
                    # A failed close cannot change an already classified operation result.
                    pass


def _is_ipv6(host: str) -> bool:
    try:
        socket.inet_pton(socket.AF_INET6, host)
        return True
    except OSError:
        return False


def _commit_transaction(connection: Any) -> None:
    transport = getattr(connection, "_socket", None)
    if transport is None:
        connection.commit()
        return
    socket = transport.sock
    if socket is None:
        raise _CommitNotSent()
    meter = _SendMeter(socket)
    transport.sock = meter
    try:
        connection.commit()
    except Exception as error:
        if meter.written_bytes == 0:
            raise _CommitNotSent() from error
        raise
    finally:
        transport.sock = socket


def _validate_statement(
    statement: str, parameters: tuple[Any, ...], allowed: frozenset[str]
) -> tuple[Any, ...]:
    try:
        statement_bytes = statement.encode("utf-8")
    except UnicodeError as error:
        raise ValueError("statement must be valid UTF-8") from error
    if not statement.strip() or len(statement_bytes) > 1 << 20 or "\x00" in statement:
        raise ValueError("statement is required and must be valid UTF-8 without NUL characters")
    leading = re.sub(
        r"\A(?:\s+|\(|/\*(?![!]|M!).*?\*/|--(?=[\s\x00-\x1f\x7f])[^\n]*\n|#[^\n]*\n)*",
        "",
        statement,
        flags=re.S,
    )
    keyword = re.match(r"[A-Za-z]+", leading)
    if keyword is None or keyword.group().upper() not in allowed:
        raise ValueError("statement has an unsupported leading keyword")
    if len(parameters) > 65535:
        raise ValueError("statement cannot bind more than 65535 parameters")
    encoded: list[Any] = []
    size = 0
    for parameter in parameters:
        value: Any
        if isinstance(parameter, bool) or parameter is None:
            value = parameter
            size += 1
        elif isinstance(parameter, int) and -(1 << 63) <= parameter < 1 << 64:
            value = parameter
            size += 8
        elif isinstance(parameter, float) and math.isfinite(parameter):
            value = parameter
            size += 8
        elif isinstance(parameter, (str, bytes)):
            value = parameter
            try:
                size += len(parameter.encode("utf-8") if isinstance(parameter, str) else parameter)
            except UnicodeError as error:
                raise ValueError("text parameter must be valid UTF-8") from error
        elif isinstance(parameter, Decimal) and parameter.is_finite():
            value = str(parameter)
            size += len(value)
        elif isinstance(parameter, datetime):
            value = parameter.astimezone(UTC).strftime("%Y-%m-%d %H:%M:%S.%f")
            size += len(value)
        else:
            raise ValueError("statement parameter has an unsupported type")
        encoded.append(value)
        if size > 16 << 20:
            raise ValueError("parameters exceed 16777216 bytes combined")
    return tuple(encoded)


def _execute_prepared(cursor: Any, statement: str, parameters: tuple[Any, ...]) -> None:
    connection = cursor._connection
    prepared = connection.cmd_stmt_prepare(
        statement.encode("utf-8"), read_timeout=cursor._read_timeout, write_timeout=cursor._write_timeout
    )
    cursor._prepared = prepared
    if len(prepared["parameters"]) != len(parameters):
        raise _ParameterCountMismatch(
            "the prepared statement parameter count does not match its placeholders"
        )
    cursor._executed = statement
    result = connection.cmd_stmt_execute(
        prepared["statement_id"],
        data=parameters,
        parameters=prepared["parameters"],
        read_timeout=cursor._read_timeout,
        write_timeout=cursor._write_timeout,
    )
    cursor._handle_result(result)


def _read_rows(cursor: Any, config: Config) -> tuple[tuple[Column, ...], tuple[dict[str, Any], ...], bool]:
    description = cursor.description or ()
    columns: list[Column] = []
    seen: set[str] = set()
    for field in description:
        name, field_type = field[0], field[1]
        if name in seen:
            raise _DuplicateColumn(f"statement returns column {name!r} more than once")
        seen.add(name)
        if len(field) < 9:
            raise _UndecodableColumn(f"column {name!r} has no character set metadata")
        type_name = _column_type_name(field_type, field[7], field[8])
        columns.append(Column(name, type_name))
    encoded_bytes = 2
    rows: list[dict[str, Any]] = []
    if not columns:
        return (), (), False
    while True:
        raw = cursor.fetchone()
        if raw is None:
            return tuple(columns), tuple(rows), False
        if len(rows) == config.max_rows:
            return tuple(columns), tuple(rows), True
        row = {}
        for column, field, value in zip(columns, description, raw, strict=True):
            try:
                row[column.name] = _decode_value(column.type_name, value, field[5])
            except (ValueError, TypeError, OverflowError, UnicodeError, struct.error) as error:
                raise _UndecodableColumn(
                    f"column {column.name!r} of type {column.type_name} "
                    "returned a value the connector cannot decode"
                ) from error
        encoded_bytes += len(json.dumps(row, ensure_ascii=False, separators=(",", ":")).encode()) + 1
        if encoded_bytes > config.max_response_bytes:
            return tuple(columns), tuple(rows), True
        rows.append(row)


def _decode_value(type_name: str, value: Any, decimals: int | None = None) -> Any:
    if value is None:
        return None
    if type_name == "BIT":
        return str(int.from_bytes(value, "big"))
    if type_name in {"BIGINT", "UNSIGNED BIGINT"}:
        return str(value)
    if type_name == "DECIMAL":
        return format(value, "f") if isinstance(value, Decimal) else str(value)
    if type_name in {
        "BINARY",
        "VARBINARY",
        "BLOB",
        "TINYBLOB",
        "MEDIUMBLOB",
        "LONGBLOB",
        "GEOMETRY",
        "VECTOR",
    }:
        return base64.b64encode(value).decode("ascii")
    if type_name == "JSON":
        return json.loads(
            value,
            parse_float=_decode_json_float,
            parse_int=_decode_json_int,
            parse_constant=lambda constant: _reject_json_constant(constant),
        )
    if type_name == "YEAR":
        if not isinstance(value, int):
            raise ValueError("invalid YEAR")
        return value
    if type_name == "FLOAT":
        if not isinstance(value, float) or not math.isfinite(value):
            raise ValueError("invalid FLOAT")
        return _shortest_float32(value)
    if isinstance(value, timedelta) and type_name == "TIME":
        micros = value // timedelta(microseconds=1)
        negative = micros < 0
        micros = abs(micros)
        hours, remainder = divmod(micros, 3_600_000_000)
        minutes, remainder = divmod(remainder, 60_000_000)
        seconds, fractional = divmod(remainder, 1_000_000)
        text = f"{'-' if negative else ''}{hours:02}:{minutes:02}:{seconds:02}"
        if decimals is not None:
            return text + (f".{fractional:06}"[: decimals + 1] if decimals not in {0, 31} else "")
        return text + (f".{fractional:06}".rstrip("0") if fractional else "")
    if isinstance(value, str) and type_name in {"DATE", "DATETIME", "TIMESTAMP"}:
        if decimals is not None and type_name != "DATE":
            base, _, fraction = value.partition(".")
            return base + ("." + fraction.ljust(6, "0")[:decimals] if decimals not in {0, 31} else "")
        return value
    if isinstance(value, (datetime, date, time, timedelta)):
        if isinstance(value, datetime):
            text = value.isoformat(timespec="microseconds").rstrip("0").rstrip(".")
            return text + ("Z" if type_name == "TIMESTAMP" else "")
        return str(value)
    if isinstance(value, Decimal):
        return format(value, "f")
    if isinstance(value, bytes):
        return value.decode("utf-8")
    return value


def _shortest_float32(value: float) -> float:
    if value == 0:
        return value
    if abs(value) == 2**-12:
        # Go chooses the upper shortest decimal for this exact halfway value.
        return math.copysign(0.00024414063, value)
    with localcontext() as context:
        context.prec = 80
        exact = Decimal.from_float(value)
        for precision in range(1, 10):
            step = Decimal(1).scaleb(exact.adjusted() - precision + 1)
            rounded = (exact / step).to_integral_value(rounding=ROUND_HALF_EVEN) * step
            candidates = (rounded - step, rounded, rounded + step)
            matches: list[Decimal] = []
            for candidate in candidates:
                try:
                    if struct.unpack("<f", struct.pack("<f", float(candidate)))[0] == value:
                        matches.append(candidate)
                except OverflowError:
                    continue
            if matches:
                selected = min(
                    matches,
                    key=lambda candidate: (abs(candidate - exact), abs(int(candidate / step)) % 2),
                )
                return float(selected)
    raise ValueError("FLOAT cannot be represented exactly")


def _decode_json_float(decimal_text: str) -> float:
    number = float(decimal_text)
    if not math.isfinite(number) or repr(number) != decimal_text:
        raise ValueError("JSON number cannot be represented without changing its text")
    return number


def _decode_json_int(integer_text: str) -> int:
    number = int(integer_text)
    if abs(number) > (1 << 53) - 1:
        raise ValueError("JSON integer exceeds the exact JSON number range")
    return number


def _reject_json_constant(constant: str) -> None:
    raise ValueError("JSON contains a nonstandard numeric constant")


def _column_type_name(field_type: int, flags: int, charset: int) -> str:
    if flags & FieldFlag.ENUM:
        return "ENUM"
    if flags & FieldFlag.SET:
        return "SET"
    type_name = _TYPE_NAMES.get(field_type, "VARCHAR")
    if field_type in {FieldType.STRING, FieldType.VAR_STRING, FieldType.VARCHAR} and charset == 63:
        return "BINARY" if field_type == FieldType.STRING else "VARBINARY"
    if field_type in {FieldType.TINY_BLOB, FieldType.BLOB, FieldType.MEDIUM_BLOB, FieldType.LONG_BLOB}:
        if charset != 63:
            return type_name.replace("BLOB", "TEXT")
    if flags & FieldFlag.UNSIGNED and type_name in {
        "TINYINT",
        "SMALLINT",
        "MEDIUMINT",
        "INT",
        "BIGINT",
    }:
        return "UNSIGNED " + type_name
    return type_name


def _classify(error: Exception, phase: str) -> _Disposition:
    if isinstance(error, _InvalidTrustedRoots):
        return _Disposition(
            "defect",
            FailureKind.VALIDATION,
            "the file named by the Worker's MYSQL_SSL_CA environment variable is "
            "unreadable or holds no PEM certificate",
        )
    if isinstance(error, _DuplicateColumn):
        return _Disposition("defect", FailureKind.VALIDATION, str(error))
    if isinstance(error, _ParameterCountMismatch):
        return _Disposition("defect", FailureKind.VALIDATION, str(error))
    if isinstance(error, _UndecodableColumn):
        return _Disposition("invalidResponse", FailureKind.PROTOCOL, str(error))
    if isinstance(error, _CommitNotSent):
        return _Disposition(
            "retry",
            FailureKind.TRANSPORT,
            "the connection ended or the call deadline passed before COMMIT was sent, "
            "so nothing was committed",
        )
    if isinstance(error, _TLSUnavailable) and phase == "connect":
        return _Disposition(
            "providerRejected",
            FailureKind.PROVIDER_REJECTION,
            "the MySQL server does not accept TLS, which sslMode requires",
        )
    if phase == "connect" and isinstance(error.__cause__, ssl.SSLCertVerificationError):
        return _Disposition(
            "providerRejected",
            FailureKind.AUTHENTICATION,
            "the MySQL server certificate failed sslMode verification",
        )
    if isinstance(error, MySQLError):
        number = error.errno or 0
        state = error.sqlstate or ""
        cause = error.__cause__
        is_dns_failure = isinstance(cause, socket.gaierror) and cause.errno in {
            socket.EAI_NONAME,
            getattr(socket, "EAI_NODATA", socket.EAI_NONAME),
        }
        if phase == "connect" and (number == 2005 or is_dns_failure):
            return _Disposition(
                "providerRejected",
                FailureKind.NOT_FOUND,
                "the MySQL host name does not resolve",
                number,
                state,
            )
        if phase in {"begin", "statement"} and number in {2006, 2013, 2055}:
            return _Disposition(
                "retry",
                FailureKind.TRANSPORT,
                "the connection failed before anything was committed",
                number,
                state,
            )
        if phase == "statement" and number == 1210:
            return _Disposition(
                "defect",
                FailureKind.VALIDATION,
                "the prepared statement parameter count does not match its placeholders",
                number,
                state,
            )
        if phase == "connect" and number in {2002, 2003, 2006, 2013, 2026, 2055}:
            return _Disposition(
                "retry",
                FailureKind.AVAILABILITY,
                "the MySQL server is unreachable or did not finish connecting in time",
                number,
                state,
            )
        if (
            phase == "commit"
            and number not in _RETRY_CONFLICT
            and state != "40001"
            and not state.startswith("23")
        ):
            return _Disposition(
                "uncertain",
                FailureKind.AVAILABILITY,
                "the server returned an error while committing, so whether the transaction "
                "committed is unknown",
                number,
                state,
            )
        if number in _RETRY_CONFLICT or state == "40001":
            branch, kind = "retry", FailureKind.CONFLICT
        elif number in _RETRY_AVAILABILITY:
            branch, kind = "retry", FailureKind.AVAILABILITY
        elif number == 1226:
            branch, kind = "providerRejected", FailureKind.QUOTA_EXHAUSTED
        elif state.startswith("08"):
            branch, kind = "retry", FailureKind.TRANSPORT
        elif number in _AUTHENTICATION or state.startswith("28"):
            branch, kind = "providerRejected", FailureKind.AUTHENTICATION
        elif number in _AUTHORIZATION or state == "25006":
            branch, kind = "providerRejected", FailureKind.AUTHORIZATION
        elif number in _NOT_FOUND or state.startswith("42S"):
            branch, kind = "providerRejected", FailureKind.NOT_FOUND
        elif number in _CONFLICT or state.startswith("23"):
            branch, kind = "providerRejected", FailureKind.CONFLICT
        elif number in _TIMEOUT:
            branch, kind = "providerRejected", FailureKind.AVAILABILITY
        elif number in _VALIDATION or state.startswith(("22", "42", "0A")):
            branch, kind = "providerRejected", FailureKind.VALIDATION
        else:
            branch, kind = "providerRejected", FailureKind.PROVIDER_REJECTION
        return _Disposition(branch, kind, f"the server returned error {number} during {phase}", number, state)
    if isinstance(error, ssl.SSLCertVerificationError) and phase == "connect":
        return _Disposition(
            "providerRejected",
            FailureKind.AUTHENTICATION,
            "the MySQL server certificate failed sslMode verification",
        )
    if isinstance(error, socket.gaierror) and phase == "connect" and error.errno == socket.EAI_NONAME:
        return _Disposition("providerRejected", FailureKind.NOT_FOUND, "the MySQL host name does not resolve")
    if isinstance(error, ValueError):
        return _Disposition(
            "invalidResponse", FailureKind.PROTOCOL, "the server returned an unreadable value"
        )
    if phase == "connect":
        return _Disposition(
            "retry",
            FailureKind.AVAILABILITY,
            "the MySQL server is unreachable or did not finish connecting in time",
        )
    if phase == "commit":
        return _Disposition(
            "uncertain",
            FailureKind.TRANSPORT,
            "the connection failed after COMMIT was sent, so whether the transaction committed is unknown",
        )
    if isinstance(error, (OSError, ConnectionError, TimeoutError)):
        return _Disposition(
            "retry", FailureKind.TRANSPORT, "the connection failed before anything was committed"
        )
    return _Disposition(
        "invalidResponse", FailureKind.PROTOCOL, "the driver could not decode the server reply"
    )


def _error_metadata(disposition: _Disposition) -> dict[str, str]:
    result = {}
    if disposition.number:
        result["errorNumber"] = str(disposition.number)
    if disposition.sql_state:
        result["sqlState"] = disposition.sql_state
    return result


def _receipt(call: Call, metadata: dict[str, str]) -> Receipt:
    return Receipt(call.id, "mysql", datetime.now(UTC), call.idempotency_key, metadata=metadata)


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "mysql", operation, message)


def _query_branch(
    branch: str, kind: FailureKind, message: str, receipt: Receipt | None = None
) -> QueryAttempt[RowSet]:
    return QueryAttempt.branch_result(
        branch, RowSet(), receipt or Receipt(), _failure("query", kind, message)
    )


def _empty_execution() -> StatementExecution:
    return StatementExecution(last_insert_id="0")


def _execute_branch(
    branch: str, kind: FailureKind, message: str, receipt: Receipt | None = None
) -> MutationAttempt[StatementExecution]:
    return MutationAttempt.branch_result(
        branch, _empty_execution(), receipt or Receipt(), _failure("execute", kind, message)
    )
