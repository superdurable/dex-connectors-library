# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Native PostgreSQL provider calls and Go-compatible attempt classification."""

import base64
import ipaddress
import json
import math
import re
import unicodedata
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any

import psycopg
from dex_connectors.sdk import (
    Call,
    CallID,
    CredentialProvider,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    QueryAttempt,
    Receipt,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import (
    COMPLETED,
    CONNECTOR_ID,
    DEFECT,
    EXECUTE_DEFINITION,
    INVALID_RESPONSE,
    LIMIT_EXCEEDED,
    PROVIDER_REJECTED,
    QUERY_DEFINITION,
    TRUNCATED,
)
from .models import Column, ExecuteStatementInput, QueryRowsInput, RowSet, StatementExecution

_TYPE_NAMES = {
    16: "bool",
    17: "bytea",
    18: "char",
    19: "name",
    20: "int8",
    21: "int2",
    23: "int4",
    25: "text",
    26: "oid",
    114: "json",
    142: "xml",
    650: "cidr",
    700: "float4",
    701: "float8",
    790: "money",
    829: "macaddr",
    869: "inet",
    1042: "bpchar",
    1043: "varchar",
    1082: "date",
    1083: "time",
    1114: "timestamp",
    1184: "timestamptz",
    1186: "interval",
    1266: "timetz",
    1560: "bit",
    1562: "varbit",
    1700: "numeric",
    2950: "uuid",
    3802: "jsonb",
    4072: "jsonpath",
}
_OWNED_KEYWORDS = {
    "ABORT",
    "BEGIN",
    "COMMIT",
    "END",
    "PREPARE",
    "RELEASE",
    "ROLLBACK",
    "SAVEPOINT",
    "START",
    "COPY",
}
_NUMERIC = re.compile(r"^(?:-?[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?|NaN|-?Infinity)$")
_INTEGER = re.compile(r"^-?[0-9]+$")


@dataclass(frozen=True, slots=True)
class _ClassifiedFailure:
    disposition: str
    kind: FailureKind
    message: str
    sql_state: str = ""


class _InvalidValue(ValueError):
    def __init__(self, column: Column) -> None:
        super().__init__(
            f'column "{column.name}" with type OID {column.type_oid} returned '
            "text the connector cannot decode"
        )


class Client:
    """One database role and a per-call credential source; no session is pooled."""

    def __init__(self, config: Config, credentials: CredentialProvider[Credentials]) -> None:
        if config is None or credentials is None:
            raise ValueError("PostgreSQL configuration and credential source are required")
        if (
            not config.host
            or config.host.strip() != config.host
            or any(character in config.host for character in ",=' \"\\")
            or "://" in config.host
            or any(unicodedata.category(character) == "Cc" for character in config.host)
        ):
            raise ValueError("PostgreSQL host must name one server")
        if (
            not config.database.strip()
            or not config.user.strip()
            or any(unicodedata.category(character) == "Cc" for character in config.database + config.user)
        ):
            raise ValueError("PostgreSQL database and user must be non-blank and contain no controls")
        if type(config.port) is not int or not 1 <= config.port <= 65535:
            raise ValueError("PostgreSQL port must be from 1 through 65535")
        if config.ssl_mode not in ("require", "verify-ca", "verify-full", "disable"):
            raise ValueError("PostgreSQL sslMode is invalid")
        if config.ssl_mode == "disable" and not _is_local_host(config.host):
            raise ValueError("PostgreSQL sslMode disable is allowed only for a local host")
        if (
            not math.isfinite(config.connect_timeout_seconds)
            or not math.isfinite(config.statement_timeout_seconds)
            or config.connect_timeout_seconds <= 0
            or config.statement_timeout_seconds < 0.001
        ):
            raise ValueError("PostgreSQL timeouts must be positive")
        if (
            type(config.max_rows) is not int
            or type(config.max_response_bytes) is not int
            or not 1 <= config.max_rows <= 100_000
            or not 1 <= config.max_response_bytes <= 16 << 20
        ):
            raise ValueError("PostgreSQL row and response limits are invalid")
        self.config = config
        self.credentials = credentials

    def query(self) -> "QueryRowsOperation":
        """Return the read-only Query operation."""
        return QueryRowsOperation(self)

    def execute(self) -> "ExecuteStatementOperation":
        """Return the transactional Mutation operation."""
        return ExecuteStatementOperation(self)

    async def _connect(self, password: str) -> psycopg.AsyncConnection[Any]:
        config = self.config
        return await psycopg.AsyncConnection.connect(
            host=config.host,
            port=config.port,
            dbname=config.database,
            user=config.user,
            password=password,
            sslmode=config.ssl_mode,
            connect_timeout=math.ceil(config.connect_timeout_seconds),
            target_session_attrs="any",
            application_name="dex-postgresql-connector",
            cursor_factory=psycopg.AsyncRawCursor,
            autocommit=True,
        )

    async def _begin(self, connection: psycopg.AsyncConnection[Any], is_read_only: bool) -> None:
        statement = "BEGIN TRANSACTION READ ONLY" if is_read_only else "BEGIN"
        settings = (
            f"SET LOCAL statement_timeout = {math.ceil(self.config.statement_timeout_seconds * 1000)}",
            "SET LOCAL idle_in_transaction_session_timeout = 30000",
            "SET LOCAL timezone = 'UTC'",
            "SET LOCAL datestyle = 'ISO, MDY'",
            "SET LOCAL intervalstyle = 'iso_8601'",
            "SET LOCAL bytea_output = 'hex'",
            "SET LOCAL extra_float_digits = 3",
        )
        async with connection.cursor() as cursor:
            await cursor.execute(statement)
            for setting in settings:
                await cursor.execute(setting)

    async def _resolve_password(self, call: Call) -> str | None:
        try:
            credentials = await self.credentials.resolve(call)
            password = credentials.password.reveal()
            return password if password else None
        except (ValueError, LookupError, OSError):
            return None


class QueryRowsOperation:
    """Run one statement in a read-only transaction."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return the generated query identity and branches."""
        return QUERY_DEFINITION

    async def invoke(self, call: Call, input: QueryRowsInput) -> QueryAttempt[RowSet]:
        """Classify one query invocation and return a bounded row set."""
        try:
            parameters = _bind_parameters(input.statement, input.parameters)
        except ValueError as error:
            return _query_failure(DEFECT, FailureKind.VALIDATION, str(error))
        password = await self.client._resolve_password(call)
        if password is None:
            return _query_failure(
                DEFECT, FailureKind.AUTHENTICATION, "PostgreSQL connection credentials are unavailable"
            )
        connection: psycopg.AsyncConnection[Any] | None = None
        phase = "connect"
        try:
            connection = await self.client._connect(password)
            phase = "begin"
            await self.client._begin(connection, True)
            phase = "statement"
            async with connection.cursor() as cursor:
                await cursor.execute(input.statement, parameters)
                columns, rows, is_truncated, _ = _read_result(cursor.pgresult, self.client.config)
            receipt = _receipt(call, connection)
            row_set = RowSet(columns=columns, rows=rows, truncated=is_truncated)
            if is_truncated:
                return QueryAttempt.branch_result(
                    TRUNCATED,
                    row_set,
                    receipt,
                    _failure(
                        "query",
                        FailureKind.RESPONSE_TOO_LARGE,
                        "the statement returned more rows or bytes than maxRows or maxResponseBytes allow",
                    ),
                )
            return QueryAttempt.branch_result(COMPLETED, row_set, receipt)
        except Exception as error:
            classified = _classify_failure(error, phase)
            if classified.disposition in ("retry", "uncertain"):
                return QueryAttempt.retry(_failure("query", classified.kind, classified.message))
            branch = (
                INVALID_RESPONSE
                if classified.disposition == "invalid"
                else (DEFECT if classified.disposition == "defect" else PROVIDER_REJECTED)
            )
            return QueryAttempt.branch_result(
                branch,
                RowSet(),
                _receipt(call, connection, classified.sql_state),
                _failure("query", classified.kind, classified.message),
            )
        finally:
            if connection is not None:
                # Closing an uncommitted transaction rolls it back; close errors cannot change the outcome.
                try:
                    await connection.close()
                except psycopg.Error:
                    pass


class ExecuteStatementOperation:
    """Run one write statement in its own transaction."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return the generated mutation identity and branches."""
        return EXECUTE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: ExecuteStatementInput) -> IdempotencyKey:
        """Use the stable Dex call ID as PostgreSQL's idempotency key."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: ExecuteStatementInput) -> MutationAttempt[StatementExecution]:
        """Commit a bounded write or report a conclusive, retryable, or uncertain outcome."""
        try:
            parameters = _bind_parameters(input.statement, input.parameters)
            if input.max_rows_affected is not None and input.max_rows_affected < 0:
                raise ValueError("maxRowsAffected cannot be negative")
            if input.idempotency_key_placeholder:
                if input.idempotency_key_placeholder != len(parameters) + 1 or len(parameters) == 65535:
                    raise ValueError("idempotencyKeyPlaceholder must be the last placeholder")
                parameters += (str(call.idempotency_key),)
        except ValueError as error:
            return _execute_failure(DEFECT, FailureKind.VALIDATION, str(error))
        password = await self.client._resolve_password(call)
        if password is None:
            return _execute_failure(
                DEFECT, FailureKind.AUTHENTICATION, "PostgreSQL connection credentials are unavailable"
            )
        connection: psycopg.AsyncConnection[Any] | None = None
        phase = "connect"
        try:
            connection = await self.client._connect(password)
            phase = "begin"
            await self.client._begin(connection, False)
            phase = "statement"
            async with connection.cursor() as cursor:
                await cursor.execute(input.statement, parameters)
                columns, rows, is_truncated, affected = _read_result(cursor.pgresult, self.client.config)
                command = (cursor.statusmessage or "").split(" ")[0]
            if is_truncated:
                return _execute_failure(
                    LIMIT_EXCEEDED,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "RETURNING rows exceeded maxRows or maxResponseBytes, so the transaction was rolled back",
                    _receipt(call, connection),
                )
            if input.max_rows_affected is not None and affected > input.max_rows_affected:
                return _execute_failure(
                    LIMIT_EXCEEDED,
                    FailureKind.CONFLICT,
                    f"the statement affected {affected} rows but maxRowsAffected is "
                    f"{input.max_rows_affected}, so the transaction was rolled back",
                    _receipt(call, connection),
                )
            if connection.closed:
                return MutationAttempt.retry(
                    _failure(
                        "execute",
                        FailureKind.TRANSPORT,
                        "the connection ended or the call deadline passed before COMMIT "
                        "was sent, so nothing was committed",
                    )
                )
            phase = "commit"
            async with connection.cursor() as cursor:
                await cursor.execute("COMMIT")
                if cursor.statusmessage != "COMMIT":
                    return _execute_failure(
                        PROVIDER_REJECTED,
                        FailureKind.PROVIDER_REJECTION,
                        "PostgreSQL rolled back the transaction at COMMIT",
                        _receipt(call, connection),
                    )
            value = StatementExecution(command=command, rows_affected=affected, columns=columns, rows=rows)
            return MutationAttempt.branch_result(COMPLETED, value, _receipt(call, connection))
        except Exception as error:
            classified = _classify_failure(error, phase)
            failure = _failure("execute", classified.kind, classified.message)
            if classified.disposition == "retry":
                return MutationAttempt.retry(failure)
            receipt = _receipt(call, connection, classified.sql_state)
            if classified.disposition == "uncertain":
                return MutationAttempt.uncertain(StatementExecution(), failure, receipt)
            branch = (
                INVALID_RESPONSE
                if classified.disposition == "invalid"
                else (DEFECT if classified.disposition == "defect" else PROVIDER_REJECTED)
            )
            return MutationAttempt.branch_result(branch, StatementExecution(), receipt, failure)
        finally:
            if connection is not None:
                # A failed close cannot alter a committed result; uncommitted writes roll back on disconnect.
                try:
                    await connection.close()
                except psycopg.Error:
                    pass


def _is_local_host(host: str) -> bool:
    if host.startswith("/") or host == "localhost":
        return True
    try:
        return ipaddress.ip_address(host).is_loopback
    except ValueError:
        return False


def _leading_keyword(statement: str) -> str:
    remaining = statement.lstrip()
    while remaining:
        if remaining.startswith("--"):
            _, _, remaining = remaining.partition("\n")
        elif remaining.startswith("/*"):
            depth = 0
            index = 0
            while index < len(remaining) - 1:
                if remaining[index : index + 2] == "/*":
                    depth += 1
                    index += 2
                elif remaining[index : index + 2] == "*/":
                    depth -= 1
                    index += 2
                    if depth == 0:
                        break
                else:
                    index += 1
            if depth:
                return ""
            remaining = remaining[index:]
        else:
            match = re.match(r"[A-Za-z]+", remaining)
            return match.group(0).upper() if match else ""
        remaining = remaining.lstrip()
    return ""


def _bind_parameters(statement: str, parameters: tuple[Any, ...]) -> tuple[str | None, ...]:
    if not isinstance(statement, str) or not statement.strip():
        raise ValueError("statement is required")
    if len(statement.encode("utf-8")) > 1 << 20 or "\x00" in statement:
        raise ValueError("statement is too long or contains NUL")
    if _leading_keyword(statement) in _OWNED_KEYWORDS:
        raise ValueError("transaction control and COPY are owned by the connector")
    if len(parameters) > 65535:
        raise ValueError("statement cannot bind more than 65535 parameters")
    encoded: list[str | None] = []
    total_bytes = 0
    for number, parameter in enumerate(parameters, 1):
        if parameter is None:
            value = None
        elif isinstance(parameter, str):
            value = parameter
        elif isinstance(parameter, bool):
            value = "true" if parameter else "false"
        elif isinstance(parameter, int):
            value = str(parameter)
        elif isinstance(parameter, float):
            value = (
                "NaN"
                if math.isnan(parameter)
                else (
                    "Infinity"
                    if parameter == math.inf
                    else "-Infinity"
                    if parameter == -math.inf
                    else repr(parameter)
                )
            )
        elif isinstance(parameter, bytes):
            value = "\\x" + parameter.hex()
        elif isinstance(parameter, datetime):
            value = parameter.isoformat()
        else:
            raise ValueError(f"parameter ${number} has an unsupported type")
        if value is not None:
            if "\x00" in value:
                raise ValueError(f"parameter ${number} text contains NUL")
            total_bytes += len(value.encode("utf-8"))
            if total_bytes > 16 << 20:
                raise ValueError("parameters exceed 16777216 bytes combined")
        encoded.append(value)
    return tuple(encoded)


def _read_result(
    result: Any, config: Config
) -> tuple[tuple[Column, ...], tuple[dict[str, Any], ...], bool, int]:
    if result is None:
        raise ValueError("PostgreSQL returned no result")
    columns = tuple(
        Column(
            result.fname(index).decode("utf-8"), _TYPE_NAMES.get(result.ftype(index), ""), result.ftype(index)
        )
        for index in range(result.nfields)
    )
    if len({column.name for column in columns}) != len(columns):
        raise ValueError("statement returns a column more than once")
    rows: list[dict[str, Any]] = []
    encoded_bytes = 2
    for row_index in range(result.ntuples):
        if len(rows) == config.max_rows:
            return columns, tuple(rows), True, result.command_tuples or 0
        row: dict[str, Any] = {}
        for column_index, column in enumerate(columns):
            row[column.name] = _decode_column(column, result.get_value(row_index, column_index))
        encoded_row = json.dumps(row, ensure_ascii=False, separators=(",", ":"))
        for character, escaped in (
            ("<", "\\u003c"),
            (">", "\\u003e"),
            ("&", "\\u0026"),
            ("\u2028", "\\u2028"),
            ("\u2029", "\\u2029"),
        ):
            encoded_row = encoded_row.replace(character, escaped)
        encoded_bytes += len(encoded_row.encode("utf-8")) + 1
        if encoded_bytes > config.max_response_bytes:
            return columns, tuple(rows), True, result.command_tuples or 0
        rows.append(row)
    return columns, tuple(rows), False, result.command_tuples or 0


def _decode_column(column: Column, raw: bytes | None) -> Any:
    if raw is None:
        return None
    try:
        value = raw.decode("utf-8")
        oid = column.type_oid
        if oid == 16:
            if value not in ("t", "f"):
                raise ValueError
            return value == "t"
        if oid in (21, 23):
            number = int(value)
            if not -(1 << 31) <= number < 1 << 31:
                raise ValueError
            return number
        if oid == 20:
            if not _INTEGER.fullmatch(value):
                raise ValueError
            return value
        if oid in (700, 701):
            float_number = float(value)
            return value if not math.isfinite(float_number) else float_number
        if oid == 1700:
            if not _NUMERIC.fullmatch(value):
                raise ValueError
            return value
        if oid == 17:
            if not value.startswith("\\x"):
                raise ValueError
            return base64.b64encode(bytes.fromhex(value[2:])).decode("ascii")
        if oid in (114, 3802):
            return json.loads(value)
        if oid == 1184:
            moment = datetime.fromisoformat(value.replace(" ", "T"))
            return moment.astimezone(UTC).isoformat().replace("+00:00", "Z")
        if oid == 1114:
            return value.replace(" ", "T", 1)
        return value
    except (UnicodeError, ValueError, OverflowError, json.JSONDecodeError) as error:
        raise _InvalidValue(column) from error


def _classify_failure(error: Exception, phase: str) -> _ClassifiedFailure:
    if isinstance(error, _InvalidValue):
        return _ClassifiedFailure("invalid", FailureKind.PROTOCOL, str(error))
    sql_state = error.sqlstate if isinstance(error, psycopg.Error) else None
    if sql_state:
        if phase == "commit" and not sql_state.startswith(("23", "40")):
            return _ClassifiedFailure(
                "uncertain",
                FailureKind.AVAILABILITY,
                f"PostgreSQL returned SQLSTATE {sql_state} while committing, "
                "so whether the transaction committed is unknown",
                sql_state,
            )
        disposition = "rejected"
        kind = FailureKind.PROVIDER_REJECTION
        if sql_state in ("40001", "40P01", "55P03"):
            disposition, kind = "retry", FailureKind.CONFLICT
        elif sql_state in ("57P01", "57P02", "57P03") or sql_state.startswith(("53", "58")):
            disposition, kind = "retry", FailureKind.AVAILABILITY
        elif sql_state.startswith("08"):
            disposition, kind = "retry", FailureKind.TRANSPORT
        elif sql_state in ("28000", "28P01"):
            kind = FailureKind.AUTHENTICATION
        elif sql_state in ("42501", "25006"):
            kind = FailureKind.AUTHORIZATION
        elif sql_state in ("3D000", "3F000", "42P01", "42703", "42704", "42883"):
            kind = FailureKind.NOT_FOUND
        elif sql_state.startswith("23"):
            kind = FailureKind.CONFLICT
        elif sql_state == "57014":
            kind = FailureKind.AVAILABILITY
        elif sql_state.startswith(("22", "42", "0A")):
            kind = FailureKind.VALIDATION
        return _ClassifiedFailure(
            disposition, kind, f"PostgreSQL returned SQLSTATE {sql_state} during {phase}", sql_state
        )
    if phase == "connect":
        message = str(error).lower()
        if "certificate verify failed" in message or "certificate verification failed" in message:
            return _ClassifiedFailure(
                "rejected",
                FailureKind.AUTHENTICATION,
                "the PostgreSQL server certificate failed sslMode verification",
            )
        if "server refused tls connection" in message or "server does not support ssl" in message:
            return _ClassifiedFailure(
                "rejected",
                FailureKind.PROVIDER_REJECTION,
                "the PostgreSQL server does not accept TLS, which sslMode requires",
            )
        if "name or service not known" in message or "nodename nor servname provided" in message:
            return _ClassifiedFailure(
                "rejected", FailureKind.NOT_FOUND, "the PostgreSQL host name does not resolve"
            )
        return _ClassifiedFailure(
            "retry",
            FailureKind.AVAILABILITY,
            "PostgreSQL is unreachable or did not finish connecting in time",
        )
    if phase == "commit":
        return _ClassifiedFailure(
            "uncertain",
            FailureKind.TRANSPORT,
            "the connection failed after COMMIT was sent, so whether the transaction committed is unknown",
        )
    if isinstance(error, (ValueError, psycopg.ProgrammingError)):
        return _ClassifiedFailure(
            "defect", FailureKind.VALIDATION, "PostgreSQL statement description is invalid"
        )
    return _ClassifiedFailure(
        "retry", FailureKind.TRANSPORT, f"the connection failed during {phase} before anything was committed"
    )


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, CONNECTOR_ID, operation, message)


def _receipt(call: Call, connection: psycopg.AsyncConnection[Any] | None, sql_state: str = "") -> Receipt:
    metadata = {"sqlState": sql_state} if sql_state else {}
    if connection is not None and connection.pgconn.backend_pid:
        metadata["backendPid"] = str(connection.pgconn.backend_pid)
        version = connection.info.parameter_status("server_version")
        if version and len(version) <= 128 and version.isascii() and version.isprintable():
            metadata["serverVersion"] = version
    return Receipt(call.id, CONNECTOR_ID, datetime.now(UTC), call.idempotency_key, metadata=metadata)


def _query_failure(branch: str, kind: FailureKind, message: str) -> QueryAttempt[RowSet]:
    return QueryAttempt.branch_result(branch, RowSet(), Receipt(), _failure("query", kind, message))


def _execute_failure(
    branch: str,
    kind: FailureKind,
    message: str,
    receipt: Receipt | None = None,
) -> MutationAttempt[StatementExecution]:
    return MutationAttempt.branch_result(
        branch,
        StatementExecution(),
        receipt or Receipt(),
        _failure("execute", kind, message),
    )
