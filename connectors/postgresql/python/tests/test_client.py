# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Native-driver decisions mirrored from the Go PostgreSQL connector tests."""

import psycopg
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    FailureKind,
    IdempotencyKey,
    OperationRef,
    SecretString,
    StaticCredentialProvider,
)

from dex_connectors.postgresql import Client, Config, Credentials, ExecuteStatementInput, QueryRowsInput
from dex_connectors.postgresql.client import _bind_parameters, _classify_failure, _decode_column, _read_result
from dex_connectors.postgresql.models import Column

REFERENCE = ConnectionRef("postgresql", "test")
CALL_ID = CallID("00000000-0000-4000-8000-000000000001")


def _call(operation: str) -> Call:
    return Call(CALL_ID, REFERENCE, OperationRef("postgresql", operation), IdempotencyKey(CALL_ID))


def _client(max_rows: int = 1000, max_response_bytes: int = 1048576) -> Client:
    config = Config(
        host="localhost",
        database="test",
        user="test",
        ssl_mode="disable",
        max_rows=max_rows,
        max_response_bytes=max_response_bytes,
    )
    credentials = StaticCredentialProvider({REFERENCE: Credentials(SecretString("placeholder"))})
    return Client(config, credentials)


class _Result:
    def __init__(
        self,
        columns: tuple[tuple[str, int], ...],
        rows: tuple[tuple[bytes | None, ...], ...],
        affected: int = 0,
    ) -> None:
        self.columns = columns
        self.rows = rows
        self.nfields = len(columns)
        self.ntuples = len(rows)
        self.command_tuples = affected

    def fname(self, index: int) -> bytes:
        return self.columns[index][0].encode()

    def ftype(self, index: int) -> int:
        return self.columns[index][1]

    def get_value(self, row: int, column: int) -> bytes | None:
        return self.rows[row][column]


class _Cursor:
    def __init__(self, connection: "_Connection") -> None:
        self.connection = connection
        self.pgresult = _Result((), ())
        self.statusmessage = ""

    async def __aenter__(self) -> "_Cursor":
        return self

    async def __aexit__(self, *_: object) -> None:
        return None

    async def execute(self, statement: str, parameters: tuple[str | None, ...] = ()) -> None:
        self.connection.statements.append((statement, parameters))
        if statement == "COMMIT":
            if self.connection.commit_error is not None:
                raise self.connection.commit_error
            self.statusmessage = "COMMIT"
        elif statement == self.connection.statement:
            if self.connection.statement_error is not None:
                raise self.connection.statement_error
            self.pgresult = self.connection.result
            self.statusmessage = self.connection.command


class _Info:
    def parameter_status(self, name: str) -> str:
        return "17.11" if name == "server_version" else ""


class _PgConn:
    backend_pid = 123


class _Connection:
    def __init__(self, statement: str, result: _Result, command: str = "SELECT 1") -> None:
        self.statement = statement
        self.result = result
        self.command = command
        self.statements: list[tuple[str, tuple[str | None, ...]]] = []
        self.statement_error: Exception | None = None
        self.commit_error: Exception | None = None
        self.closed = False
        self.info = _Info()
        self.pgconn = _PgConn()

    def cursor(self) -> _Cursor:
        return _Cursor(self)

    async def close(self) -> None:
        self.closed = True


@pytest.mark.parametrize(
    ("code", "phase", "disposition", "kind"),
    [
        ("40001", "statement", "retry", FailureKind.CONFLICT),
        ("40P01", "statement", "retry", FailureKind.CONFLICT),
        ("55P03", "statement", "retry", FailureKind.CONFLICT),
        ("57P01", "statement", "retry", FailureKind.AVAILABILITY),
        ("53300", "statement", "retry", FailureKind.AVAILABILITY),
        ("58030", "statement", "retry", FailureKind.AVAILABILITY),
        ("08006", "statement", "retry", FailureKind.TRANSPORT),
        ("28P01", "connect", "rejected", FailureKind.AUTHENTICATION),
        ("42501", "statement", "rejected", FailureKind.AUTHORIZATION),
        ("25006", "statement", "rejected", FailureKind.AUTHORIZATION),
        ("42P01", "statement", "rejected", FailureKind.NOT_FOUND),
        ("23505", "statement", "rejected", FailureKind.CONFLICT),
        ("57014", "statement", "rejected", FailureKind.AVAILABILITY),
        ("22P02", "statement", "rejected", FailureKind.VALIDATION),
        ("42601", "statement", "rejected", FailureKind.VALIDATION),
        ("57P01", "commit", "uncertain", FailureKind.AVAILABILITY),
        ("23505", "commit", "rejected", FailureKind.CONFLICT),
        ("40001", "commit", "retry", FailureKind.CONFLICT),
    ],
)
def test_sqlstate_classification(code: str, phase: str, disposition: str, kind: FailureKind) -> None:
    error = psycopg.errors.lookup(code)("secret from server")
    classified = _classify_failure(error, phase)
    assert (classified.disposition, classified.kind, classified.sql_state) == (disposition, kind, code)
    assert "secret" not in classified.message


def test_transport_and_conclusive_connect_classification() -> None:
    assert _classify_failure(OSError("connection refused"), "connect").disposition == "retry"
    assert (
        _classify_failure(OSError("certificate verify failed"), "connect").kind == FailureKind.AUTHENTICATION
    )
    assert _classify_failure(OSError("name or service not known"), "connect").kind == FailureKind.NOT_FOUND
    assert _classify_failure(OSError("lost"), "statement").disposition == "retry"
    assert _classify_failure(OSError("lost"), "commit").disposition == "uncertain"


@pytest.mark.parametrize(
    "statement", ["", "BEGIN", "-- comment\nCOMMIT", "/* outer /* inner */ end */ COPY t FROM STDIN"]
)
def test_connector_owned_statements_are_defects(statement: str) -> None:
    with pytest.raises(ValueError):
        _bind_parameters(statement, ())


def test_parameter_encoding_and_type_mapping() -> None:
    assert _bind_parameters("SELECT $1", (None, "text", True, 42, 1.5, b"\x00\x01\xff")) == (
        None,
        "text",
        "true",
        "42",
        "1.5",
        "\\x0001ff",
    )
    assert _decode_column(Column("big", "int8", 20), b"9007199254740993") == "9007199254740993"
    assert _decode_column(Column("exact", "numeric", 1700), b"250.00") == "250.00"
    assert _decode_column(Column("payload", "bytea", 17), b"\\x0001ff") == "AAH/"
    assert _decode_column(Column("flag", "bool", 16), b"t") is True


@pytest.mark.parametrize(
    "host", ["", " db.example.com", 'db"example.com', "a,b", "https://db.example.com", "db\x7f"]
)
def test_connection_rejects_invalid_host(host: str) -> None:
    credentials = StaticCredentialProvider({REFERENCE: Credentials(SecretString("placeholder"))})
    with pytest.raises(ValueError, match="host"):
        Client(Config(host, "test", "test"), credentials)


def test_bounded_rows_and_duplicate_columns() -> None:
    result = _Result((("n", 23),), ((b"1",), (b"2",), (b"3",)))
    columns, rows, truncated, _ = _read_result(result, _client(max_rows=2).config)
    assert len(columns) == 1 and rows == ({"n": 1}, {"n": 2}) and truncated
    duplicate = _Result((("id", 23), ("id", 23)), ())
    with pytest.raises(ValueError, match="more than once"):
        _read_result(duplicate, _client().config)


@pytest.mark.asyncio
async def test_query_branches_and_no_commit(monkeypatch: pytest.MonkeyPatch) -> None:
    statement = "SELECT $1::int4 AS n"
    connection = _Connection(statement, _Result((("n", 23),), ((b"1",), (b"2",))))

    async def connect(_: Client, password: str) -> _Connection:
        assert password == "placeholder"
        return connection

    monkeypatch.setattr(Client, "_connect", connect)
    result = await _client(max_rows=1).query().invoke(_call("query"), QueryRowsInput(statement, (1,)))
    assert result.branch == "truncated" and result.value.rows == ({"n": 1},)
    assert all(sql != "COMMIT" for sql, _ in connection.statements)
    assert connection.closed


@pytest.mark.asyncio
async def test_execute_limits_commit_and_uncertainty(monkeypatch: pytest.MonkeyPatch) -> None:
    statement = "INSERT INTO t VALUES ($1, $2) RETURNING id"
    connection = _Connection(statement, _Result((("id", 23),), ((b"1",),), 1), "INSERT 0 1")

    async def connect(_: Client, password: str) -> _Connection:
        connection.closed = False
        return connection

    monkeypatch.setattr(Client, "_connect", connect)
    input = ExecuteStatementInput(statement, ("item",), 2, 1)
    completed = await _client().execute().invoke(_call("execute"), input)
    assert completed.branch == "completed" and completed.value.rows_affected == 1
    assert connection.statements[-2][1] == ("item", str(CALL_ID))
    assert connection.statements[-1][0] == "COMMIT"
    connection.statements.clear()
    limited = (
        await _client().execute().invoke(_call("execute"), ExecuteStatementInput(statement, ("item",), 2, 0))
    )
    assert limited.branch == "limitExceeded"
    assert all(sql != "COMMIT" for sql, _ in connection.statements)
    connection.commit_error = OSError("lost after send")
    uncertain = await _client().execute().invoke(_call("execute"), input)
    assert uncertain.kind == "uncertain" and uncertain.branch == "uncertain"


@pytest.mark.asyncio
async def test_missing_credentials_are_terminal() -> None:
    client = Client(Config("localhost", "test", "test", ssl_mode="disable"), StaticCredentialProvider({}))
    query = await client.query().invoke(_call("query"), QueryRowsInput("SELECT 1"))
    execute = await client.execute().invoke(
        _call("execute"), ExecuteStatementInput("INSERT INTO t VALUES (1)")
    )
    assert query.branch == "defect" and query.failure.kind == FailureKind.AUTHENTICATION
    assert execute.branch == "defect" and execute.failure.kind == FailureKind.AUTHENTICATION


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("operation", "error", "expected_kind", "expected_branch"),
    [
        ("query", psycopg.errors.SyntaxError("secret SQL"), "branch", "providerRejected"),
        ("execute", psycopg.errors.UniqueViolation("secret value"), "branch", "providerRejected"),
        ("query", OSError("connection dropped"), "retry", ""),
        ("execute", OSError("connection dropped"), "retry", ""),
    ],
)
async def test_statement_driver_failures_select_go_routes(
    monkeypatch: pytest.MonkeyPatch,
    operation: str,
    error: Exception,
    expected_kind: str,
    expected_branch: str,
) -> None:
    statement = "SELECT 1" if operation == "query" else "INSERT INTO t VALUES (1)"
    connection = _Connection(statement, _Result((), ()))
    connection.statement_error = error

    async def connect(_: Client, password: str) -> _Connection:
        return connection

    monkeypatch.setattr(Client, "_connect", connect)
    if operation == "query":
        outcome = await _client().query().invoke(_call(operation), QueryRowsInput(statement))
    else:
        outcome = await _client().execute().invoke(_call(operation), ExecuteStatementInput(statement))
    assert outcome.kind == expected_kind and outcome.branch == expected_branch
    assert outcome.failure is not None and "secret" not in outcome.failure.message
    assert all(sql != "COMMIT" for sql, _ in connection.statements)


@pytest.mark.asyncio
@pytest.mark.parametrize("operation", ["query", "execute"])
async def test_undecodable_server_value_selects_invalid_response(
    monkeypatch: pytest.MonkeyPatch,
    operation: str,
) -> None:
    statement = "SELECT flag FROM t" if operation == "query" else "UPDATE t SET flag = true RETURNING flag"
    connection = _Connection(statement, _Result((("flag", 16),), ((b"not-a-bool",),), 1), "UPDATE 1")

    async def connect(_: Client, password: str) -> _Connection:
        return connection

    monkeypatch.setattr(Client, "_connect", connect)
    if operation == "query":
        outcome = await _client().query().invoke(_call(operation), QueryRowsInput(statement))
    else:
        outcome = await _client().execute().invoke(_call(operation), ExecuteStatementInput(statement))
    assert outcome.branch == "invalidResponse" and outcome.failure.kind == FailureKind.PROTOCOL
    assert all(sql != "COMMIT" for sql, _ in connection.statements)


@pytest.mark.asyncio
async def test_closed_connection_before_commit_retries(monkeypatch: pytest.MonkeyPatch) -> None:
    statement = "UPDATE t SET flag = true"
    connection = _Connection(statement, _Result((), (), 1), "UPDATE 1")

    async def connect(_: Client, password: str) -> _Connection:
        return connection

    original_read_result = _read_result

    def close_before_commit(
        result: _Result, config: Config
    ) -> tuple[tuple[Column, ...], tuple[dict[str, object], ...], bool, int]:
        connection.closed = True
        return original_read_result(result, config)

    monkeypatch.setattr(Client, "_connect", connect)
    monkeypatch.setattr("dex_connectors.postgresql.client._read_result", close_before_commit)
    outcome = await _client().execute().invoke(_call("execute"), ExecuteStatementInput(statement))
    assert outcome.kind == "retry" and outcome.failure.kind == FailureKind.TRANSPORT
    assert all(sql != "COMMIT" for sql, _ in connection.statements)
