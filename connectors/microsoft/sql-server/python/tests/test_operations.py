# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Native SQL Server operation decisions without a database dependency."""

import asyncio
import socket
from dataclasses import dataclass
from datetime import UTC, datetime, time
from decimal import Decimal
from pathlib import Path
from uuid import UUID, uuid4

import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    FailureKind,
    IdempotencyKey,
    OperationRef,
    SecretString,
)

from dex_connectors.microsoft_sql_server import (
    Client,
    Config,
    Credentials,
    ExecuteStatementInput,
    QueryRowsInput,
)
from dex_connectors.microsoft_sql_server.operations import _bind_statement, _classify, _read_rows


class CredentialsSource:
    async def resolve(self, _call: Call) -> Credentials:
        return Credentials(SecretString("test-only-password"))


@dataclass
class FakeCursor:
    description: tuple = (("value", 127, None, None, None, None, None),)
    rowcount: int = 1
    rows: tuple = ((17,),)
    failure: Exception | None = None
    fail_on: str | None = None

    def execute(self, statement: str, _parameters: object = None) -> None:
        if self.failure and (self.fail_on is None or self.fail_on in statement):
            raise self.failure

    def fetchone(self) -> tuple | None:
        if not self.rows:
            return None
        row, *remaining = self.rows
        self.rows = tuple(remaining)
        return row

    def close(self) -> None:
        pass


class FakeConnection:
    def __init__(self, cursor: FakeCursor, commit_failure: Exception | None = None) -> None:
        self.test_cursor = cursor
        self.commit_failure = commit_failure
        self.committed = False
        self.rolled_back = False
        self.closed = False

    def cursor(self) -> FakeCursor:
        return self.test_cursor

    def commit(self) -> None:
        if self.commit_failure:
            raise self.commit_failure
        self.committed = True

    def rollback(self) -> None:
        self.rolled_back = True

    def close(self) -> None:
        self.closed = True


def call(operation: str) -> Call:
    call_id = CallID(str(uuid4()))
    return Call(
        call_id,
        ConnectionRef("microsoft-sql-server", "test"),
        OperationRef("microsoft-sql-server", operation),
        IdempotencyKey(str(call_id)),
    )


def client(connection: FakeConnection, *, max_rows: int = 1000) -> Client:
    return Client(
        Config("localhost", "master", "sa", encrypt="disable", max_rows=max_rows),
        CredentialsSource(),
        connection_factory=lambda **_kwargs: connection,
    )


@pytest.mark.parametrize(
    ("number", "phase", "disposition", "kind"),
    [
        (18456, "connect", "rejected", FailureKind.AUTHENTICATION),
        (40615, "connect", "rejected", FailureKind.AUTHENTICATION),
        (229, "statement", "rejected", FailureKind.AUTHORIZATION),
        (208, "statement", "rejected", FailureKind.NOT_FOUND),
        (2627, "statement", "rejected", FailureKind.CONFLICT),
        (245, "statement", "rejected", FailureKind.VALIDATION),
        (40544, "statement", "rejected", FailureKind.QUOTA_EXHAUSTED),
        (1205, "statement", "retry", FailureKind.CONFLICT),
        (1222, "statement", "retry", FailureKind.CONFLICT),
        (40501, "statement", "retry", FailureKind.AVAILABILITY),
        (50999, "statement", "retry", FailureKind.AVAILABILITY),
        (50000, "statement", "rejected", FailureKind.PROVIDER_REJECTION),
        (3999, "commit", "uncertain", FailureKind.AVAILABILITY),
        (3902, "commit", "rejected", FailureKind.CONFLICT),
    ],
)
def test_driver_number_classification(number: int, phase: str, disposition: str, kind: FailureKind) -> None:
    error = RuntimeError("secret provider message")
    error.number = number  # type: ignore[attr-defined]
    error.state = 3  # type: ignore[attr-defined]
    error.severity = 21 if number == 50999 else 14  # type: ignore[attr-defined]
    actual, actual_kind, message, metadata = _classify(error, phase)
    assert (actual, actual_kind, metadata["errorNumber"]) == (disposition, kind, str(number))
    assert "secret provider message" not in message


def test_transport_and_commit_loss_classification() -> None:
    assert _classify(ConnectionRefusedError(), "connect")[0] == "retry"
    assert _classify(EOFError(), "statement")[0] == "retry"
    assert _classify(EOFError(), "commit")[0] == "uncertain"
    assert _classify(socket.gaierror(socket.EAI_NONAME, "not found"), "connect")[1] == FailureKind.NOT_FOUND


def test_sql_binding_rejects_dangerous_and_unbound_statements() -> None:
    for statement in ("SELECT 1; DELETE FROM dbo.accounts", "SELECT @other", "SELECT @p01", "EXEC dbo.proc"):
        with pytest.raises(ValueError):
            _bind_statement(statement, (1,), False)
    statement, values = _bind_statement("SELECT @p1 AS n, @p1 AS duplicate", (7,), False)
    assert statement == "SELECT %(p1)s AS n, %(p1)s AS duplicate"
    assert values == {"p1": 7}


def test_row_codec_preserves_exact_database_values() -> None:
    cursor = FakeCursor(
        description=(
            ("big", 127),
            ("exact", 106),
            ("identifier", 36),
            ("moment", 43),
            ("payload", 165),
            ("single", 59),
        ),
        rows=(
            (
                9007199254740993,
                Decimal("250.00"),
                UUID("6f1c2a9e-0000-4000-8000-000000000001"),
                datetime(2026, 1, 1, 9, 0, 0, 500000, UTC),
                b"\x00\xff",
                0.10000000149011612,
            ),
        ),
    )
    columns, rows, truncated = _read_rows(cursor, 1000, 1 << 20)
    assert [column.type_name for column in columns] == [
        "BIGINT",
        "DECIMAL",
        "UNIQUEIDENTIFIER",
        "DATETIMEOFFSET",
        "VARBINARY",
        "REAL",
    ]
    assert rows == (
        {
            "big": "9007199254740993",
            "exact": "250.00",
            "identifier": "6f1c2a9e-0000-4000-8000-000000000001",
            "moment": "2026-01-01T09:00:00.5Z",
            "payload": "AP8=",
            "single": 0.1,
        },
    )
    assert not truncated


@pytest.mark.parametrize("type_code", [41, 42, 43])
def test_scale_seven_temporal_columns_fail_before_emitting_lossy_rows(type_code: int) -> None:
    cursor = FakeCursor(
        description=(("exact_time", type_code, None, None, 7, None, None),),
        rows=((datetime(2026, 1, 1, 9, 0, 0, 123456, UTC),),),
    )
    outcome = asyncio.run(
        client(FakeConnection(cursor))
        .query()
        .invoke(call("query"), QueryRowsInput("SELECT @p1 AS exact_time", ("2026-01-01",)))
    )
    assert outcome.branch == "invalidResponse"
    assert "exact_time" in outcome.failure.message
    assert cursor.rows


def test_scale_six_temporal_columns_keep_their_exact_microseconds() -> None:
    cursor = FakeCursor(
        description=(("clock", 41, None, None, 6, None, None), ("moment", 42, None, None, 6, None, None)),
        rows=((time(12, 34, 56, 500000), datetime(2026, 1, 1, 9, 0, 0, 123456)),),
    )
    _, rows, truncated = _read_rows(cursor, 1000, 1 << 20)
    assert rows == ({"clock": "12:34:56.5", "moment": "2026-01-01T09:00:00.123456"},)
    assert not truncated


def test_configuration_rejects_unsupported_encryption_before_connecting() -> None:
    for encryption in ("strict", "trust-server-certificate"):
        with pytest.raises(ValueError, match="does not support"):
            Client(Config("localhost", "master", "sa", encrypt=encryption), CredentialsSource())


def test_zero_configuration_fields_take_manifest_defaults() -> None:
    native = Client(
        Config(
            "localhost",
            "master",
            "sa",
            port=0,
            encrypt="",
            connect_timeout_seconds=0,
            statement_timeout_seconds=0,
            max_rows=0,
            max_response_bytes=0,
        ),
        CredentialsSource(),
    )
    assert native.config.port == 1433
    assert native.config.encrypt == "mandatory"
    assert native.config.max_rows == 1000


def test_mandatory_tls_supplies_per_connection_ca_and_hostname_check(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    certificate = tmp_path / "ca.pem"
    certificate.write_text("inert CA fixture")
    monkeypatch.setenv("SQLSERVER_SSL_CA", str(certificate))
    seen: dict[str, object] = {}

    def connect(**settings: object) -> FakeConnection:
        seen.update(settings)
        return FakeConnection(FakeCursor())

    native = Client(
        Config("db.example.com", "app", "dex_user"), CredentialsSource(), connection_factory=connect
    )
    connection = native.open_connection("inert")
    connection.close()
    assert seen["cafile"] == str(certificate)
    assert seen["validate_host"] is True
    assert seen["enc_login_only"] is False
    assert seen["disable_connect_retry"] is True


def test_query_completed_and_truncated() -> None:
    completed_connection = FakeConnection(FakeCursor())
    completed = asyncio.run(
        client(completed_connection)
        .query()
        .invoke(call("query"), QueryRowsInput("SELECT @p1 AS value", (17,)))
    )
    assert completed.branch == "completed"
    assert completed.value.rows == ({"value": "17"},)
    assert completed_connection.rolled_back and completed_connection.closed
    truncated_connection = FakeConnection(FakeCursor(rows=((17,), (18,))))
    truncated = asyncio.run(
        client(truncated_connection, max_rows=1)
        .query()
        .invoke(call("query"), QueryRowsInput("SELECT @p1 AS value", (17,)))
    )
    assert truncated.branch == "truncated"
    assert truncated.value.truncated


def test_execute_limit_rolls_back_and_commit_loss_is_uncertain() -> None:
    limited_connection = FakeConnection(FakeCursor(description=(), rowcount=2))
    limited = asyncio.run(
        client(limited_connection)
        .execute()
        .invoke(call("execute"), ExecuteStatementInput("UPDATE dbo.t SET x = @p1", (1,), max_rows_affected=1))
    )
    assert limited.branch == "limitExceeded"
    assert not limited_connection.committed
    uncertain_connection = FakeConnection(FakeCursor(description=()), EOFError())
    uncertain = asyncio.run(
        client(uncertain_connection)
        .execute()
        .invoke(call("execute"), ExecuteStatementInput("UPDATE dbo.t SET x = @p1", (1,)))
    )
    assert uncertain.kind == "uncertain"


def test_native_query_and_execute_failure_branches() -> None:
    rejected_error = RuntimeError("secret value")
    rejected_error.number = 208  # type: ignore[attr-defined]
    rejected_error.state = 1  # type: ignore[attr-defined]
    rejected_error.severity = 16  # type: ignore[attr-defined]
    rejected_connection = FakeConnection(FakeCursor(failure=rejected_error, fail_on="SELECT"))
    rejected = asyncio.run(
        client(rejected_connection)
        .query()
        .invoke(call("query"), QueryRowsInput("SELECT @p1 AS value", (17,)))
    )
    assert rejected.branch == "providerRejected"
    assert rejected.failure.kind == FailureKind.NOT_FOUND
    assert "secret value" not in rejected.failure.message

    retry_error = RuntimeError("deadlock details")
    retry_error.number = 1205  # type: ignore[attr-defined]
    retry_error.state = 1  # type: ignore[attr-defined]
    retry_error.severity = 13  # type: ignore[attr-defined]
    retry_connection = FakeConnection(FakeCursor(failure=retry_error, fail_on="UPDATE"))
    retry = asyncio.run(
        client(retry_connection)
        .execute()
        .invoke(call("execute"), ExecuteStatementInput("UPDATE dbo.t SET x = @p1", (1,)))
    )
    assert retry.kind == "retry"
    assert retry.failure.kind == FailureKind.CONFLICT

    invalid_connection = FakeConnection(
        FakeCursor(description=(("value", 98, None, None, None, None, None),))
    )
    invalid = asyncio.run(
        client(invalid_connection).query().invoke(call("query"), QueryRowsInput("SELECT @p1 AS value", (17,)))
    )
    assert invalid.branch == "invalidResponse"

    missing_result_connection = FakeConnection(FakeCursor(description=()))
    defect = asyncio.run(
        client(missing_result_connection)
        .query()
        .invoke(call("query"), QueryRowsInput("SELECT @p1 AS value", (17,)))
    )
    assert defect.branch == "defect"


def test_execute_completed_and_output_bound() -> None:
    completed_connection = FakeConnection(FakeCursor(description=(), rowcount=1))
    completed = asyncio.run(
        client(completed_connection)
        .execute()
        .invoke(call("execute"), ExecuteStatementInput("UPDATE dbo.t SET x = @p1", (1,)))
    )
    assert completed.branch == "completed"
    assert completed.value.rows_affected == 1
    assert completed_connection.committed

    bounded_connection = FakeConnection(FakeCursor(rows=((17,), (18,))))
    bounded = asyncio.run(
        client(bounded_connection, max_rows=1)
        .execute()
        .invoke(
            call("execute"),
            ExecuteStatementInput(
                "UPDATE dbo.t SET x = @p1 OUTPUT inserted.x", (1,), returns_output_rows=True
            ),
        )
    )
    assert bounded.branch == "limitExceeded"
    assert bounded_connection.rolled_back
    assert not bounded_connection.committed


def test_query_retry_and_execute_rejection_invalid_response_and_defect() -> None:
    retry_error = RuntimeError("provider details")
    retry_error.number = 1205  # type: ignore[attr-defined]
    retry_error.state = 1  # type: ignore[attr-defined]
    retry_error.severity = 13  # type: ignore[attr-defined]
    retry_connection = FakeConnection(FakeCursor(failure=retry_error, fail_on="SELECT"))
    retry = asyncio.run(
        client(retry_connection).query().invoke(call("query"), QueryRowsInput("SELECT @p1 AS value", (17,)))
    )
    assert retry.kind == "retry"

    rejected_error = RuntimeError("provider details")
    rejected_error.number = 2627  # type: ignore[attr-defined]
    rejected_error.state = 1  # type: ignore[attr-defined]
    rejected_error.severity = 16  # type: ignore[attr-defined]
    rejected_connection = FakeConnection(FakeCursor(failure=rejected_error, fail_on="UPDATE"))
    rejected = asyncio.run(
        client(rejected_connection)
        .execute()
        .invoke(call("execute"), ExecuteStatementInput("UPDATE dbo.t SET x = @p1", (1,)))
    )
    assert rejected.branch == "providerRejected"
    assert rejected.failure.kind == FailureKind.CONFLICT

    invalid_connection = FakeConnection(
        FakeCursor(description=(("value", 98, None, None, None, None, None),))
    )
    invalid = asyncio.run(
        client(invalid_connection)
        .execute()
        .invoke(
            call("execute"),
            ExecuteStatementInput(
                "UPDATE dbo.t SET x = @p1 OUTPUT inserted.x", (1,), returns_output_rows=True
            ),
        )
    )
    assert invalid.branch == "invalidResponse"

    defect_connection = FakeConnection(FakeCursor(description=()))
    defect = asyncio.run(
        client(defect_connection)
        .execute()
        .invoke(
            call("execute"),
            ExecuteStatementInput(
                "UPDATE dbo.t SET x = @p1 OUTPUT inserted.x", (1,), returns_output_rows=True
            ),
        )
    )
    assert defect.branch == "defect"


def test_credentials_unavailable_is_terminal() -> None:
    class MissingCredentials:
        async def resolve(self, _call: Call) -> Credentials:
            raise ValueError("secret")

    operation = Client(Config("localhost", "master", "sa", encrypt="disable"), MissingCredentials()).query()
    outcome = asyncio.run(operation.invoke(call("query"), QueryRowsInput("SELECT 1")))
    assert outcome.branch == "defect"
    assert outcome.failure.kind == FailureKind.AUTHENTICATION
