# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise each operation's branch, retry, and uncertain result."""

from dataclasses import replace
from datetime import UTC, datetime, timedelta
from typing import Any
from uuid import uuid4

import pytest
from dex_connectors.sdk import Call, CallID, ConnectionRef, OperationRef, RepeatOnUnknown, SecretString
from dex_connectors.sdk.singledispatch import Marker, State
from mysql.connector.constants import FieldType
from mysql.connector.errors import DatabaseError

from dex_connectors.mysql import Client, Config, Credentials, ExecuteStatementInput, QueryRowsInput
from dex_connectors.mysql._generated.definitions import (
    EXECUTE_DEFINITION,
    EXECUTE_NON_IDEMPOTENT_DEFINITION,
)


class _Credentials:
    async def resolve(self, call: Call) -> Credentials:
        return Credentials(SecretString("inert-password"))


class _Cursor:
    def __init__(
        self,
        rows: list[tuple[Any, ...]] | None = None,
        description: tuple[Any, ...] | None = None,
        error: Exception | None = None,
        rows_affected: int = 1,
        last_insert_id: int = 1,
        warning_count: int = 0,
    ) -> None:
        self.rows = list(rows or [])
        self.description = description
        self.error = error
        self.rowcount = rows_affected
        self.lastrowid = last_insert_id
        self.warning_count = warning_count
        self.sql: list[str] = []
        self.did_close = False
        self._connection: _Connection | None = None
        self._read_timeout = None
        self._write_timeout = None
        self._prepared: dict[str, Any] | None = None
        self._executed = ""

    def _handle_result(self, result: object) -> None:
        pass

    def execute(self, statement: str, parameters: tuple[Any, ...] = ()) -> None:
        self.sql.append(statement)
        if self.error:
            raise self.error

    def fetchone(self) -> tuple[Any, ...] | None:
        return self.rows.pop(0) if self.rows else None

    def close(self) -> None:
        self.did_close = True


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
            self.prepared._connection = self
            return self.prepared
        if not self.transaction.sql:
            return self.transaction
        return self.outcome

    def start_transaction(self) -> None:
        self.transaction.sql.append("START TRANSACTION")

    def cmd_stmt_prepare(self, statement: bytes, **kwargs: object) -> dict[str, Any]:
        return {"statement_id": 1, "parameters": [object()] * statement.count(b"?")}

    def cmd_stmt_execute(self, statement_id: int, data: tuple[Any, ...], **kwargs: object) -> dict[str, Any]:
        self.prepared.execute(self.prepared._executed, data)
        return {
            "affected_rows": self.prepared.rowcount,
            "insert_id": self.prepared.lastrowid,
            "warning_count": self.prepared.warning_count,
        }

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
    column = ("id", FieldType.LONGLONG, None, None, None, None, None, 0, 63)
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
        _Cursor([(1,)], (("id", FieldType.LONG, None, None, None, None, None, 0, 63),) * 2)
    )
    duplicate = (
        await _client(monkeypatch, duplicate_connection)
        .query()
        .invoke(_call("query"), QueryRowsInput("SELECT id, id FROM t"))
    )
    assert duplicate.branch == "defect"
    invalid_connection = _Connection(
        _Cursor([(b"\xff",)], (("title", FieldType.VAR_STRING, None, None, None, None, None, 0, 46),))
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
async def test_execute_success_limit_and_retry_of_an_unknown_commit(monkeypatch: pytest.MonkeyPatch) -> None:
    input = ExecuteStatementInput("INSERT INTO t VALUES (?)", (1,))
    connection = _Connection(_Cursor())
    completed = await _client(monkeypatch, connection).execute().invoke(_call("execute"), input)
    assert completed.branch == "completed" and connection.did_commit
    assert completed.value is not None and completed.value.rows_affected == 1
    limit_connection = _Connection(_Cursor(rows_affected=2))
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
        _Cursor([(b"\xff",)], (("title", FieldType.VAR_STRING, None, None, None, None, None, 0, 46),))
    )
    invalid = await _client(monkeypatch, invalid_connection).execute().invoke(_call("execute"), input)
    assert invalid.branch == "invalidResponse"
    defect = (
        await _client(monkeypatch, connection)
        .execute()
        .invoke(_call("execute"), ExecuteStatementInput("CREATE TABLE t (id INT)"))
    )
    assert defect.branch == "defect"
    unknown_connection = _Connection(_Cursor(), commit_error=ConnectionResetError())
    unknown = await _client(monkeypatch, unknown_connection).execute().invoke(_call("execute"), input)
    # execute is repeat-safe, so an unknown COMMIT runs again instead of selecting uncertain.
    assert unknown.kind == "retry"
    assert unknown.failure is not None and unknown.failure.kind.value == "TRANSPORT"
    assert unknown_connection.did_close


@pytest.mark.asyncio
async def test_execute_uses_prepared_server_outcome_before_cursor_close(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    prepared = _Cursor(rows_affected=2, last_insert_id=8, warning_count=1)
    connection = _Connection(prepared)
    client = _client(monkeypatch, connection)
    limited = await client.execute().invoke(
        _call("execute"), ExecuteStatementInput("UPDATE t SET value = 1", max_rows_affected=1)
    )
    assert limited.branch == "limitExceeded" and not connection.did_commit
    prepared = _Cursor(rows_affected=2, last_insert_id=8, warning_count=1)
    connection = _Connection(prepared)
    completed = (
        await _client(monkeypatch, connection)
        .execute()
        .invoke(_call("execute"), ExecuteStatementInput("UPDATE t SET value = 1"))
    )
    assert completed.branch == "completed" and completed.value is not None
    assert (completed.value.rows_affected, completed.value.last_insert_id, completed.value.warning_count) == (
        2,
        "8",
        1,
    )
    assert prepared.did_close


@pytest.mark.asyncio
async def test_lossy_json_names_column_and_fails_terminally(monkeypatch: pytest.MonkeyPatch) -> None:
    column = ("amounts", FieldType.JSON, None, None, None, None, None, 0, 63)
    connection = _Connection(_Cursor([('{"n":1.10}',)], (column,)))
    attempt = (
        await _client(monkeypatch, connection)
        .query()
        .invoke(_call("query"), QueryRowsInput("SELECT amounts FROM t"))
    )
    assert attempt.branch == "invalidResponse" and attempt.failure is not None
    assert "amounts" in attempt.failure.message


class _AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self) -> None:
        self.attempt = 1
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.marker: Marker | None = None
        self.read_error: Exception | None = None

    def has_last_heartbeat_value(self) -> bool:
        if self.read_error is not None:
            raise self.read_error
        return self.marker is not None

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        return self.marker

    async def heartbeat(self, value: object = None) -> None:
        self.marker = value if isinstance(value, Marker) else None


_BALANCE_INPUT = ExecuteStatementInput(
    "UPDATE balances SET amount = amount + ? WHERE account_id = ?", ("25.00", "a")
)


def _fenced_call(context: _AttemptContext) -> Call:
    call = _call("executeNonIdempotent")
    return replace(call, context=context)  # type: ignore[arg-type]


def test_execute_tiers_are_declared() -> None:
    assert EXECUTE_DEFINITION.repeat_on_unknown == RepeatOnUnknown.SAFE
    assert EXECUTE_NON_IDEMPOTENT_DEFINITION.repeat_on_unknown == RepeatOnUnknown.UNSAFE


@pytest.mark.asyncio
async def test_non_idempotent_claims_before_commit_and_commits_once(monkeypatch: pytest.MonkeyPatch) -> None:
    connection = _Connection(_Cursor())
    context = _AttemptContext()
    call = _fenced_call(context)
    outcome = await _client(monkeypatch, connection).execute_non_idempotent().invoke(call, _BALANCE_INPUT)
    assert outcome.kind == "branch" and outcome.branch == "completed" and connection.did_commit
    assert context.marker is not None and context.marker.state == State.CLAIMED
    assert connection.did_close


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "commit_error",
    [ConnectionResetError(), DatabaseError("unknown", errno=1105, sqlstate="HY000")],
    ids=["connection lost", "server error at COMMIT"],
)
async def test_non_idempotent_selects_uncertain_and_never_runs_again(
    monkeypatch: pytest.MonkeyPatch, commit_error: Exception
) -> None:
    connection = _Connection(_Cursor(), commit_error=commit_error)
    context = _AttemptContext()
    call = _fenced_call(context)
    client = _client(monkeypatch, connection)
    first = await client.execute_non_idempotent().invoke(call, _BALANCE_INPUT)
    assert first.kind == "uncertain" and first.failure is not None
    assert first.failure.operation == "executeNonIdempotent"

    opened: list[str] = []
    monkeypatch.setattr(client, "_open", lambda password: opened.append(password))
    context.attempt = 2
    again = await client.execute_non_idempotent().invoke(call, _BALANCE_INPUT)
    assert again.kind == "uncertain"
    assert opened == [], "a later attempt never connects"


@pytest.mark.asyncio
async def test_non_idempotent_resends_only_after_a_proven_rollback(monkeypatch: pytest.MonkeyPatch) -> None:
    deadlock = _Connection(_Cursor(), commit_error=DatabaseError("deadlock", errno=1213, sqlstate="40001"))
    context = _AttemptContext()
    call = _fenced_call(context)
    first = await _client(monkeypatch, deadlock).execute_non_idempotent().invoke(call, _BALANCE_INPUT)
    assert first.kind == "retry"
    assert context.marker is not None and context.marker.state == State.CAN_RESEND

    healthy = _Connection(_Cursor())
    context.attempt = 2
    second = await _client(monkeypatch, healthy).execute_non_idempotent().invoke(call, _BALANCE_INPUT)
    assert second.branch == "completed" and healthy.did_commit

    lost = _Connection(_Cursor(error=ConnectionResetError()))
    lost_context = _AttemptContext()
    retried = (
        await _client(monkeypatch, lost)
        .execute_non_idempotent()
        .invoke(_fenced_call(lost_context), _BALANCE_INPUT)
    )
    assert retried.kind == "retry" and not lost.did_commit
    assert lost_context.marker is not None and lost_context.marker.state == State.NOT_SENT


@pytest.mark.asyncio
async def test_non_idempotent_lost_worker_and_unreadable_checkpoint(monkeypatch: pytest.MonkeyPatch) -> None:
    connection = _Connection(_Cursor())
    client = _client(monkeypatch, connection)
    opened: list[str] = []
    monkeypatch.setattr(client, "_open", lambda password: opened.append(password))
    after_lost_worker = _AttemptContext()
    after_lost_worker.attempt = 2
    lost_worker = await client.execute_non_idempotent().invoke(
        _fenced_call(after_lost_worker), _BALANCE_INPUT
    )
    assert lost_worker.kind == "uncertain"
    unreadable = _AttemptContext()
    unreadable.read_error = OSError("heartbeat store unavailable")
    retried = await client.execute_non_idempotent().invoke(_fenced_call(unreadable), _BALANCE_INPUT)
    assert retried.kind == "retry" and retried.failure is not None
    assert retried.failure.kind.value == "AVAILABILITY"
    assert opened == []


@pytest.mark.asyncio
async def test_non_idempotent_rejects_a_definite_refusal_without_committing(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    rejected = _Connection(_Cursor(error=DatabaseError("secret value", errno=3819, sqlstate="HY000")))
    context = _AttemptContext()
    outcome = (
        await _client(monkeypatch, rejected)
        .execute_non_idempotent()
        .invoke(_fenced_call(context), _BALANCE_INPUT)
    )
    assert outcome.branch == "providerRejected" and not rejected.did_commit
    assert outcome.failure is not None and "secret" not in outcome.failure.message
    assert context.marker is not None and context.marker.state == State.NOT_SENT

    deferred = _Connection(_Cursor(), commit_error=DatabaseError("duplicate", errno=1062, sqlstate="23000"))
    deferred_context = _AttemptContext()
    at_commit = (
        await _client(monkeypatch, deferred)
        .execute_non_idempotent()
        .invoke(_fenced_call(deferred_context), _BALANCE_INPUT)
    )
    assert at_commit.branch == "providerRejected"
    assert deferred_context.marker is not None and deferred_context.marker.state == State.CAN_RESEND

    limited = _Connection(_Cursor(rows_affected=3))
    limit = (
        await _client(monkeypatch, limited)
        .execute_non_idempotent()
        .invoke(
            _fenced_call(_AttemptContext()),
            ExecuteStatementInput(_BALANCE_INPUT.statement, _BALANCE_INPUT.parameters, max_rows_affected=1),
        )
    )
    assert limit.branch == "limitExceeded" and not limited.did_commit
    defect = (
        await _client(monkeypatch, limited)
        .execute_non_idempotent()
        .invoke(_fenced_call(_AttemptContext()), ExecuteStatementInput("CREATE TABLE t (id INT)"))
    )
    assert defect.branch == "defect"
