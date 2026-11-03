# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise each operation's branch, retry, and uncertain result."""

from typing import Any
from uuid import uuid4

import pytest
from dex_connectors.sdk import Call, CallID, ConnectionRef, OperationRef, SecretString
from mysql.connector.constants import FieldType
from mysql.connector.errors import DatabaseError

from dex_connectors.mysql import Client, Config, Credentials, ExecuteStatementInput, QueryRowsInput


class _Credentials:
    async def resolve(self, call: Call) -> Credentials:
        return Credentials(SecretString("inert-password"))


class _Cursor:
    def __init__(
        self,
        rows: list[tuple[Any, ...]] | None = None,
        description: tuple[Any, ...] | None = None,
        error: Exception | None = None,
    ) -> None:
        self.rows = list(rows or [])
        self.description = description
        self.error = error
        self.sql: list[str] = []

    def execute(self, statement: str, parameters: tuple[Any, ...] = ()) -> None:
        self.sql.append(statement)
        if self.error:
            raise self.error

    def fetchone(self) -> tuple[Any, ...] | None:
        return self.rows.pop(0) if self.rows else None

    def close(self) -> None:
        pass


class _Connection:
    def __init__(
        self, prepared: _Cursor, outcome: _Cursor | None = None, commit_error: Exception | None = None
    ) -> None:
        self.prepared = prepared
        self.outcome = outcome or _Cursor([(1, 1, 0)])
        self.transaction = _Cursor()
        self.commit_error = commit_error
        self.did_commit = False
        self.did_close = False

    def cursor(self, prepared: bool = False) -> _Cursor:
        if prepared:
            return self.prepared
        if not self.transaction.sql:
            return self.transaction
        return self.outcome

    def start_transaction(self) -> None:
        self.transaction.sql.append("START TRANSACTION")

    def commit(self) -> None:
        if self.commit_error:
            raise self.commit_error
        self.did_commit = True

    def close(self) -> None:
        self.did_close = True


def _client(monkeypatch: pytest.MonkeyPatch, connection: _Connection, max_rows: int = 1000) -> Client:
    client = Client(
        Config("localhost", "app", "dex_app", ssl_mode="disabled", max_rows=max_rows), _Credentials()
    )
    monkeypatch.setattr(client, "_open", lambda password: connection)
    monkeypatch.setattr(client, "_session", lambda session: ("101", "8.4.0"))
    return client


def test_client_applies_go_zero_defaults_and_rejects_remote_plaintext() -> None:
    defaulted = Client(
        Config(
            "localhost",
            "app",
            "dex_app",
            port=0,
            ssl_mode="",
            connect_timeout_seconds=0,
            statement_timeout_seconds=0,
            max_rows=0,
            max_response_bytes=0,
        ),
        _Credentials(),
    )
    assert defaulted.config.port == 3306
    assert defaulted.config.ssl_mode == "required"
    assert defaulted.config.max_rows == 1000
    with pytest.raises(ValueError):
        Client(Config("db.example.com", "app", "dex_app", ssl_mode="disabled"), _Credentials())


def _call(operation: str) -> Call:
    identifier = CallID(str(uuid4()))
    return Call(
        identifier,
        ConnectionRef("mysql", "test"),
        OperationRef("mysql", operation),
        idempotency_key=identifier,
    )


@pytest.mark.asyncio
async def test_query_success_and_row_truncation(monkeypatch: pytest.MonkeyPatch) -> None:
    column = ("id", FieldType.LONGLONG, None, None, None, None, None, 0)
    connection = _Connection(_Cursor([(1,), (2,)], (column,)))
    attempt = (
        await _client(monkeypatch, connection, max_rows=1)
        .query()
        .invoke(_call("query"), QueryRowsInput("SELECT id FROM t"))
    )
    assert attempt.kind == "branch" and attempt.branch == "truncated"
    assert attempt.value is not None and len(attempt.value.rows) == 1
    assert not connection.did_commit and connection.did_close
    connection = _Connection(_Cursor([(1,)], (column,)))
    completed = (
        await _client(monkeypatch, connection)
        .query()
        .invoke(_call("query"), QueryRowsInput("SELECT id FROM t"))
    )
    assert completed.branch == "completed"
    assert completed.value is not None and completed.value.rows == ({"id": "1"},)


@pytest.mark.asyncio
async def test_query_rejection_invalid_response_and_defect(monkeypatch: pytest.MonkeyPatch) -> None:
    rejected_connection = _Connection(_Cursor(error=DatabaseError("secret", errno=1146, sqlstate="42S02")))
    rejected = (
        await _client(monkeypatch, rejected_connection)
        .query()
        .invoke(_call("query"), QueryRowsInput("SELECT * FROM missing"))
    )
    assert rejected.branch == "providerRejected" and rejected.failure is not None
    assert rejected.failure.kind.value == "NOT_FOUND"
    duplicate_connection = _Connection(
        _Cursor([(1,)], (("id", FieldType.LONG, None, None, None, None, None, 0),) * 2)
    )
    duplicate = (
        await _client(monkeypatch, duplicate_connection)
        .query()
        .invoke(_call("query"), QueryRowsInput("SELECT id, id FROM t"))
    )
    assert duplicate.branch == "defect"
    invalid_connection = _Connection(
        _Cursor([(b"\xff",)], (("title", FieldType.VAR_STRING, None, None, None, None, None, 0),))
    )
    invalid = (
        await _client(monkeypatch, invalid_connection)
        .query()
        .invoke(_call("query"), QueryRowsInput("SELECT title FROM t"))
    )
    assert invalid.branch == "invalidResponse"
    defect = (
        await _client(monkeypatch, invalid_connection)
        .query()
        .invoke(_call("query"), QueryRowsInput("DELETE FROM t"))
    )
    assert defect.branch == "defect"


@pytest.mark.asyncio
async def test_execute_success_limit_retry_and_uncertain(monkeypatch: pytest.MonkeyPatch) -> None:
    input = ExecuteStatementInput("INSERT INTO t VALUES (?)", (1,))
    connection = _Connection(_Cursor())
    completed = await _client(monkeypatch, connection).execute().invoke(_call("execute"), input)
    assert completed.branch == "completed" and connection.did_commit
    assert completed.value is not None and completed.value.rows_affected == 1
    limit_connection = _Connection(_Cursor(), _Cursor([(2, 1, 0)]))
    limited = (
        await _client(monkeypatch, limit_connection)
        .execute()
        .invoke(
            _call("execute"), ExecuteStatementInput(input.statement, input.parameters, max_rows_affected=1)
        )
    )
    assert limited.branch == "limitExceeded" and not limit_connection.did_commit
    retry_connection = _Connection(_Cursor(error=DatabaseError("deadlock", errno=1213, sqlstate="40001")))
    retry = await _client(monkeypatch, retry_connection).execute().invoke(_call("execute"), input)
    assert retry.kind == "retry"
    rejected_connection = _Connection(_Cursor(error=DatabaseError("duplicate", errno=1062, sqlstate="23000")))
    rejected = await _client(monkeypatch, rejected_connection).execute().invoke(_call("execute"), input)
    assert rejected.branch == "providerRejected"
    invalid_connection = _Connection(
        _Cursor([(b"\xff",)], (("title", FieldType.VAR_STRING, None, None, None, None, None, 0),))
    )
    invalid = await _client(monkeypatch, invalid_connection).execute().invoke(_call("execute"), input)
    assert invalid.branch == "invalidResponse"
    defect = (
        await _client(monkeypatch, connection)
        .execute()
        .invoke(_call("execute"), ExecuteStatementInput("CREATE TABLE t (id INT)"))
    )
    assert defect.branch == "defect"
    uncertain_connection = _Connection(_Cursor(), commit_error=ConnectionResetError())
    uncertain = await _client(monkeypatch, uncertain_connection).execute().invoke(_call("execute"), input)
    assert uncertain.kind == "uncertain"
    assert uncertain.failure is not None and uncertain.failure.kind.value == "TRANSPORT"
