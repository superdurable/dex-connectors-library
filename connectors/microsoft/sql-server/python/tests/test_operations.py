# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Native SQL Server operation decisions without a database dependency."""

import asyncio
import importlib
import socket
from dataclasses import dataclass
from datetime import UTC, datetime, time, timedelta
from decimal import Decimal
from ipaddress import ip_address
from pathlib import Path
from types import SimpleNamespace
from uuid import UUID, uuid4

import certifi
import pytest
from cryptography import x509
from cryptography.hazmat.primitives import hashes
from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.x509.oid import NameOID
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    FailureKind,
    IdempotencyKey,
    OperationRef,
    SecretString,
)
from OpenSSL import SSL, crypto
from pytds import tds_base  # type: ignore[import-untyped]

from dex_connectors.microsoft_sql_server import (
    Client,
    Config,
    Credentials,
    ExecuteStatementInput,
    QueryRowsInput,
)
from dex_connectors.microsoft_sql_server.client import _verify_server_hostname
from dex_connectors.microsoft_sql_server.operations import (
    _AttemptDeadline,
    _bind_statement,
    _classify,
    _read_rows,
    _ReadBudgetExceeded,
    _ReadBudgetTransport,
)


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
    remaining_counts: tuple[int, ...] = ()

    def execute(self, statement: str, _parameters: object = None) -> None:
        if self.failure and (self.fail_on is None or self.fail_on in statement):
            raise self.failure

    def fetchone(self) -> tuple | None:
        if not self.rows:
            return None
        row, *remaining = self.rows
        self.rows = tuple(remaining)
        return row

    def nextset(self) -> bool:
        if not self.remaining_counts:
            return False
        self.rowcount, *remaining = self.remaining_counts
        self.remaining_counts = tuple(remaining)
        return True

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


def client(connection: FakeConnection, *, max_rows: int = 1000, max_response_bytes: int = 1 << 20) -> Client:
    return Client(
        Config(
            "localhost", "master", "sa", encrypt="disable", max_rows=max_rows,
            max_response_bytes=max_response_bytes,
        ),
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
    error = tds_base.DatabaseError("secret provider message")
    error.number = number
    error.state = 3
    error.severity = 21 if number == 50999 else 14
    actual, actual_kind, message, metadata = _classify(error, phase)
    assert (actual, actual_kind, metadata["errorNumber"]) == (disposition, kind, str(number))
    assert "secret provider message" not in message


def test_fatal_severity_does_not_override_known_error_group() -> None:
    for number, kind in (
        (18456, FailureKind.AUTHENTICATION),
        (229, FailureKind.AUTHORIZATION),
        (208, FailureKind.NOT_FOUND),
        (245, FailureKind.VALIDATION),
        (2627, FailureKind.CONFLICT),
    ):
        error = tds_base.DatabaseError("server detail")
        error.number = number
        error.severity = 21
        assert _classify(error, "statement")[:2] == ("rejected", kind)


def test_transport_and_commit_loss_classification() -> None:
    assert _classify(ConnectionRefusedError(), "connect")[0] == "retry"
    assert _classify(EOFError(), "statement")[0] == "retry"
    assert _classify(EOFError(), "commit")[0] == "uncertain"
    assert _classify(socket.gaierror(socket.EAI_NONAME, "not found"), "connect")[1] == FailureKind.NOT_FOUND
    assert _classify(tds_base.ClosedConnectionError(), "statement")[:2] == ("retry", FailureKind.TRANSPORT)
    assert _classify(tds_base.DataError("out of range"), "statement")[:2] == (
        "defect",
        FailureKind.VALIDATION,
    )
    assert _classify(tds_base.Error("You requested encryption but it is not supported by server"), "connect")[
        :2
    ] == ("rejected", FailureKind.PROVIDER_REJECTION)
    assert (
        _classify(tds_base.Error("Server returned unexpected ENCRYPT_ON value"), "connect")[0] == "rejected"
    )
    assert _classify(tds_base.Error("Connection closed"), "statement")[:2] == ("retry", FailureKind.TRANSPORT)
    assert _classify(SSL.ZeroReturnError(), "statement")[:2] == ("retry", FailureKind.TRANSPORT)
    assert _classify(SSL.Error("certificate verify failed"), "connect")[:2] == (
        "rejected",
        FailureKind.AUTHENTICATION,
    )
    assert _classify(TimeoutError(), "statement")[:2] == ("rejected", FailureKind.AVAILABILITY)
    assert _classify(TimeoutError(), "commit")[0] == "uncertain"


def test_sql_binding_rejects_dangerous_and_unbound_statements() -> None:
    for statement in ("SELECT 1; DELETE FROM dbo.accounts", "SELECT @other", "SELECT @p01", "EXEC dbo.proc"):
        with pytest.raises(ValueError):
            _bind_statement(statement, (1,), False)
    statement, values = _bind_statement("SELECT @p1 AS n, @p1 AS duplicate", (7,), False)
    assert statement == "SELECT %(p1)s AS n, %(p1)s AS duplicate"
    assert values == {"p1": 7}


@pytest.mark.parametrize(
    "statement",
    [
        "1 SELECT 1",
        "@p1 SELECT 1",
        "SELECT @P1",
        "SELECT @p01",
        "SELECT @other",
        "SELECT 1; DELETE FROM dbo.t",
        "SELECT 'x' /* nested /* COMMIT */ */; DELETE FROM dbo.t",
        "SELECT 1; EXEC dbo.proc",
        "SELECT 1 /* unterminated",
        "SELECT 'unterminated",
    ],
)
def test_sql_lexer_rejects_invalid_statement(statement: str) -> None:
    with pytest.raises(ValueError):
        _bind_statement(statement, (1,) if "@p" in statement.lower() else (), False)


def test_sql_lexer_accepts_literals_identifiers_hints_and_trailing_semicolons() -> None:
    statement, values = _bind_statement(
        "(SELECT N'EXEC; @p9 %' AS [COMMIT], \"DROP\" FROM dbo.t "
        "WHERE n = @p1 AND @@ROWCOUNT >= 0xDEAD /* nested /* DROP */ */ OPTION (USE HINT ('x'));;",
        (1,),
        False,
    )
    assert "%(p1)s" in statement
    assert "%%" in statement
    assert values == {"p1": 1}
    assert _bind_statement(
        "MERGE dbo.t USING dbo.s ON 1=0 WHEN NOT MATCHED THEN INSERT (n) VALUES (@p1)", (1,), True
    )[1] == {"p1": 1}
    assert _bind_statement("SELECT '100%' AS p", (), False)[0] == "SELECT '100%%' AS p"


def test_sql_parameter_limits_and_unsigned_rejection() -> None:
    with pytest.raises(ValueError):
        _bind_statement("SELECT @p1", (1 << 63,), False)
    with pytest.raises(ValueError):
        _bind_statement("SELECT @p1", (b"x" * ((16 << 20) + 1),), False)
    with pytest.raises(ValueError):
        _bind_statement("SELECT 1", tuple(range(2099)), False)
    assert _bind_statement("SELECT @p1", (None,), False)[1] == {"p1": None}


def test_row_codec_preserves_exact_database_values() -> None:
    cursor = FakeCursor(
        description=(
            ("big", 127),
            ("exact", 106, None, None, 12, 2, None),
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


def test_driver_decimal_scale_is_restored_from_column_metadata() -> None:
    cursor = FakeCursor(
        description=(
            ("fraction", 106, None, None, 12, 2, None),
            ("whole", 108, None, None, 12, 2, None),
            ("zero", 106, None, None, 12, 2, None),
            ("money", 60, None, None, None, 4, None),
            ("small_money", 122, None, None, None, 4, None),
        ),
        rows=(
            (
                Decimal(150) / Decimal(100),
                Decimal(200) / Decimal(100),
                Decimal(0) / Decimal(100),
                Decimal(125000) / Decimal(10000),
                Decimal(-12345) / Decimal(10000),
            ),
        ),
    )
    _, rows, _ = _read_rows(cursor, 10, 10000)
    assert rows == (
        {
            "fraction": "1.50",
            "whole": "2.00",
            "zero": "0.00",
            "money": "12.5000",
            "small_money": "-1.2345",
        },
    )


def test_unknown_row_count_never_bypasses_guard() -> None:
    connection = FakeConnection(FakeCursor(description=(), rowcount=-1))
    outcome = asyncio.run(
        client(connection)
        .execute()
        .invoke(call("execute"), ExecuteStatementInput("UPDATE dbo.t SET x = @p1", (1,), max_rows_affected=1))
    )
    assert outcome.branch == "invalidResponse"
    assert not connection.committed


def test_clr_bytes_keep_driver_type_name_and_unknown_type_fails() -> None:
    cursor = FakeCursor(
        description=(("location", 240, None, None, None, None, None),), rows=((b"\x00\xff",),)
    )
    cursor._session = SimpleNamespace(
        res_info=SimpleNamespace(
            columns=[SimpleNamespace(serializer=SimpleNamespace(type_name="GEOGRAPHY"))]
        ),
        _reader=None,
    )
    columns, rows, _ = _read_rows(cursor, 10, 1000)
    assert columns[0].type_name == "GEOGRAPHY"
    assert rows == ({"location": "AP8="},)
    with pytest.raises(Exception, match="unsupported SQL type"):
        _read_rows(FakeCursor(description=(("unknown", 255),)), 10, 1000)


def test_text_binary_date_and_null_values_are_serialized_without_loss() -> None:
    cursor = FakeCursor(
        description=(
            ("fixed", 47),
            ("xml_value", 241),
            ("binary_value", 173),
            ("date_value", 40),
            ("time_value", 41, None, None, 6, 6, None),
            ("small_date_time", 58),
            ("nullable", 167),
        ),
        rows=(
            (
                "A ",
                "<root />",
                b"\x00\xff",
                datetime(2026, 1, 1).date(),
                time(9, 1, 2, 123456),
                datetime(2026, 1, 1, 9, 1),
                None,
            ),
        ),
    )
    _, rows, _ = _read_rows(cursor, 10, 10000)
    assert rows == (
        {
            "fixed": "A ",
            "xml_value": "<root />",
            "binary_value": "AP8=",
            "date_value": "2026-01-01",
            "time_value": "09:01:02.123456",
            "small_date_time": "2026-01-01T09:01:00",
            "nullable": None,
        },
    )


def test_multiple_result_sets_are_rejected() -> None:
    class MultipleResultsCursor(FakeCursor):
        def nextset(self) -> bool:
            return True

    with pytest.raises(ValueError, match="more than one result set"):
        _read_rows(MultipleResultsCursor(), 10, 1000)


def test_native_read_budget_stops_a_large_value_before_buffering_it() -> None:
    class ChunkTransport:
        def recv_into(self, buffer: bytearray, length: int) -> int:
            buffer[:length] = b"x" * length
            return length

    budgeted = _ReadBudgetTransport(ChunkTransport(), 5)
    buffer = bytearray(4)
    assert budgeted.recv_into(buffer, 4) == 4
    with pytest.raises(Exception, match="read budget"):
        budgeted.recv_into(buffer, 4)
    with pytest.raises(_ReadBudgetExceeded):
        budgeted.recv_into(buffer, 1)


def test_completed_read_keeps_a_control_budget() -> None:
    reader = SimpleNamespace(_transport=SimpleNamespace())
    cursor = FakeCursor()
    cursor._session = SimpleNamespace(_reader=reader)
    _read_rows(cursor, 1000, 1000)
    assert isinstance(reader._transport, _ReadBudgetTransport)
    assert reader._transport.byte_limit == 4 << 20
    assert not reader._transport.is_poisoned


def test_read_budget_truncates_rows_and_poisons_transport() -> None:
    class ChunkTransport:
        def recv_into(self, buffer: bytearray, length: int) -> int:
            buffer[:length] = b"x" * length
            return length

    class BudgetCursor(FakeCursor):
        def fetchone(self) -> tuple | None:
            self._session._reader._transport.recv_into(bytearray(40 << 10), 40 << 10)
            return super().fetchone()

    original_transport = ChunkTransport()
    reader = SimpleNamespace(_transport=original_transport)
    cursor = BudgetCursor(rows=((17,), (18,)))
    cursor._session = SimpleNamespace(_reader=reader, process_end=lambda _marker: None)
    columns, rows, truncated = _read_rows(cursor, 1000, 100)
    assert columns[0].name == "value"
    assert rows == ({"value": "17"},)
    assert truncated
    assert reader._transport.is_poisoned
    with pytest.raises(_ReadBudgetExceeded):
        reader._transport.recv_into(bytearray(1), 1)

    reader = SimpleNamespace(_transport=ChunkTransport())
    mutation_connection = FakeConnection(cursor)
    cursor._session._reader = reader
    cursor.rows = ((17,), (18,))
    outcome = asyncio.run(
        client(mutation_connection, max_response_bytes=100)
        .execute()
        .invoke(
            call("execute"),
            ExecuteStatementInput(
                "UPDATE dbo.t SET x = @p1 OUTPUT inserted.x", (1,), returns_output_rows=True
            ),
        )
    )
    assert outcome.branch == "limitExceeded"
    assert not mutation_connection.committed
    assert reader._transport.is_poisoned
    assert not mutation_connection.rolled_back


def test_commit_read_budget_failure_is_uncertain() -> None:
    connection = FakeConnection(FakeCursor(description=()), _ReadBudgetExceeded("commit reply too large"))
    outcome = asyncio.run(
        client(connection)
        .execute()
        .invoke(call("execute"), ExecuteStatementInput("UPDATE dbo.t SET x = @p1", (1,)))
    )
    assert outcome.kind == "uncertain"
    assert not connection.rolled_back


@pytest.mark.parametrize(
    ("bytes_accepted", "expected_kind"),
    [(0, "retry"), (1, "uncertain"), (6, "uncertain")],
)
@pytest.mark.parametrize("is_tls_wrapped", [False, True])
def test_commit_send_failure_uses_actual_bytes_accepted(
    bytes_accepted: int, expected_kind: str, is_tls_wrapped: bool
) -> None:
    class CountedSocket:
        def __init__(self) -> None:
            self.accepted = 0

        def send(self, data: memoryview, _flags: int) -> int:
            accepted = min(len(data), bytes_accepted - self.accepted)
            self.accepted += accepted
            return accepted

    class SendConnection(FakeConnection):
        def __init__(self) -> None:
            super().__init__(FakeCursor(description=()))
            raw_transport = CountedSocket()
            if is_tls_wrapped:
                class EncryptedTransport:
                    def __init__(self, transport: CountedSocket) -> None:
                        self._transport = transport

                    def sendall(self, data: bytes) -> None:
                        self._transport.sendall(data)

                writer_transport = EncryptedTransport(raw_transport)
            else:
                writer_transport = raw_transport
            writer = SimpleNamespace(_transport=writer_transport)
            self._tds_socket = SimpleNamespace(main_session=SimpleNamespace(_writer=writer))

        def commit(self) -> None:
            self._tds_socket.main_session._writer._transport.sendall(b"COMMIT")
            raise EOFError("commit response was lost")

    connection = SendConnection()
    outcome = asyncio.run(
        client(connection)
        .execute()
        .invoke(call("execute"), ExecuteStatementInput("UPDATE dbo.t SET x = @p1", (1,)))
    )
    assert outcome.kind == expected_kind
    if bytes_accepted == 0:
        assert outcome.failure.kind == FailureKind.TRANSPORT
    assert connection.closed


def test_precommit_deadline_retries_when_commit_was_not_sent(monkeypatch: pytest.MonkeyPatch) -> None:
    connection = FakeConnection(FakeCursor(description=()))
    native_client = client(connection)
    original_check = _AttemptDeadline.check
    call_count = 0

    def check_at_commit(deadline: _AttemptDeadline) -> None:
        nonlocal call_count
        if deadline.timer.interval > native_client.config.statement_timeout_seconds:
            call_count += 1
            if call_count == 5:
                raise TimeoutError("attempt deadline")
        original_check(deadline)

    monkeypatch.setattr(_AttemptDeadline, "check", check_at_commit)
    outcome = asyncio.run(
        native_client.execute().invoke(
            call("execute"), ExecuteStatementInput("UPDATE dbo.t SET x = @p1", (1,))
        )
    )
    assert outcome.kind == "retry"
    assert outcome.failure.kind == FailureKind.TRANSPORT
    assert "COMMIT was not sent" in outcome.failure.message
    assert not connection.committed


def test_acknowledged_commit_stays_completed_when_deadline_expires() -> None:
    class AcknowledgedConnection(FakeConnection):
        def commit(self) -> None:
            super().commit()
            for deadline in active_deadlines:
                deadline.expired.set()

    connection = AcknowledgedConnection(FakeCursor(description=()))
    active_deadlines: list[_AttemptDeadline] = []
    native_client = client(connection)
    original_init = _AttemptDeadline.__init__

    def capture_deadline(deadline: _AttemptDeadline, seconds: float) -> None:
        original_init(deadline, seconds)
        active_deadlines.append(deadline)

    with pytest.MonkeyPatch.context() as monkeypatch:
        monkeypatch.setattr(_AttemptDeadline, "__init__", capture_deadline)
        outcome = asyncio.run(
            native_client.execute().invoke(
                call("execute"), ExecuteStatementInput("UPDATE dbo.t SET x = @p1", (1,))
            )
        )
    assert outcome.branch == "completed"
    assert connection.committed


def test_multiple_done_counts_include_trigger_rows() -> None:
    connection = FakeConnection(FakeCursor(description=(), rowcount=1, remaining_counts=(2,)))
    outcome = asyncio.run(
        client(connection)
        .execute()
        .invoke(call("execute"), ExecuteStatementInput("UPDATE dbo.t SET x = @p1", (1,), max_rows_affected=2))
    )
    assert outcome.branch == "limitExceeded"
    assert not connection.committed


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


@pytest.mark.parametrize("host", ["db host", "db'host", 'db"host', "db=host", "db\nhost", "tcp:db"])
def test_host_rejects_invalid_address_syntax(host: str) -> None:
    with pytest.raises(ValueError):
        Client(Config(host, "master", "sa"), CredentialsSource())


def test_statement_timeout_rejects_sql_server_lock_timeout_overflow() -> None:
    with pytest.raises(ValueError, match="LOCK_TIMEOUT"):
        Client(
            Config("localhost", "master", "sa", statement_timeout_seconds=((1 << 31) / 1000)),
            CredentialsSource(),
        )


def test_explicit_missing_ca_file_fails_closed(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("SQLSERVER_SSL_CA", "/missing/ca.pem")
    native = Client(Config("localhost", "master", "sa"), CredentialsSource())
    with pytest.raises(ValueError, match="trusted CA bundle"):
        native.open_connection("inert")


def test_explicit_ca_file_without_certificates_is_a_defect(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    certificate = tmp_path / "ca.pem"
    certificate.write_text("not a PEM certificate")
    monkeypatch.setenv("SQLSERVER_SSL_CA", str(certificate))
    native = Client(Config("localhost", "master", "sa"), CredentialsSource())
    with pytest.raises(ValueError, match="trusted CA bundle is unreadable"):
        native.open_connection("inert")


def test_mandatory_tls_supplies_per_connection_ca_and_hostname_check(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    certificate = tmp_path / "ca.pem"
    certificate.write_bytes(Path(certifi.where()).read_bytes())
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
    assert seen["timeout"] == (
        native.config.connect_timeout_seconds + native.config.statement_timeout_seconds + 5
    )


def test_attempt_deadline_attaches_before_session_settings() -> None:
    deadline = _AttemptDeadline(10)
    connection: FakeConnection

    class SettingsCursor(FakeCursor):
        def execute(self, statement: str, _parameters: object = None) -> None:
            assert deadline.connection is connection

    connection = FakeConnection(SettingsCursor())
    native = Client(
        Config("localhost", "master", "sa", encrypt="disable"),
        CredentialsSource(),
        connection_factory=lambda **_settings: connection,
    )
    try:
        assert native.open_connection("inert", deadline) is connection
    finally:
        deadline.close()


def test_attempt_deadline_shuts_down_socket_during_login(monkeypatch: pytest.MonkeyPatch) -> None:
    client_module = importlib.import_module("dex_connectors.microsoft_sql_server.client")
    deadline = _AttemptDeadline(10)
    observed: dict[str, object] = {}

    class LoginSocket:
        def shutdown(self, _how: int) -> None:
            observed["shut_down"] = True

    login_socket = LoginSocket()

    def connect(_address: object, timeout: float) -> LoginSocket:
        observed["timeout"] = timeout
        return login_socket

    monkeypatch.setattr(client_module, "_ORIGINAL_SOCKET_CONNECTION", connect)
    client_module._TLS_HOST_VALIDATION.deadline = deadline
    try:
        assert client_module._dispatch_socket_connection(("localhost", 1433), 30) is login_socket
        assert 0 < observed["timeout"] < 10
        deadline._expire()
        assert observed["shut_down"] is True
    finally:
        client_module._TLS_HOST_VALIDATION.deadline = None
        deadline.close()


def test_invalid_session_identity_is_a_protocol_failure(monkeypatch: pytest.MonkeyPatch) -> None:
    client_module = importlib.import_module("dex_connectors.microsoft_sql_server.client")
    statements: list[str] = []

    class IdentityCursor(FakeCursor):
        def execute(self, statement: str, _parameters: object = None) -> None:
            statements.append(statement)

    cursor = IdentityCursor(rows=(("invalid", "16.0"),))
    connection = FakeConnection(cursor)

    def connect(**_settings: object) -> FakeConnection:
        return connection

    monkeypatch.setattr(client_module.pytds, "connect", connect)
    native = Client(
        Config("localhost", "master", "sa", encrypt="disable"),
        CredentialsSource(),
        connection_factory=connect,
    )
    with pytest.raises(Exception, match="session identity") as failure:
        native.open_connection("inert")
    assert _classify(failure.value, "connect")[0] == "invalid"
    assert len(statements) == 1
    assert "SET LOCK_TIMEOUT" in statements[0] and "SELECT @@SPID" in statements[0]
    assert connection.closed


def test_ca_signed_certificate_uses_san_precedence_dns_case_and_ip_san() -> None:
    ca_key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    ca_name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "Test CA")])
    ca_certificate = (
        x509.CertificateBuilder()
        .subject_name(ca_name)
        .issuer_name(ca_name)
        .public_key(ca_key.public_key())
        .serial_number(x509.random_serial_number())
        .not_valid_before(datetime.now(UTC) - timedelta(days=1))
        .not_valid_after(datetime.now(UTC) + timedelta(days=1))
        .add_extension(x509.BasicConstraints(ca=True, path_length=None), critical=True)
        .sign(ca_key, hashes.SHA256())
    )
    leaf_key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    leaf_certificate = (
        x509.CertificateBuilder()
        .subject_name(x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "wrong.example.com")]))
        .issuer_name(ca_certificate.subject)
        .public_key(leaf_key.public_key())
        .serial_number(x509.random_serial_number())
        .not_valid_before(datetime.now(UTC) - timedelta(days=1))
        .not_valid_after(datetime.now(UTC) + timedelta(days=1))
        .add_extension(
            x509.SubjectAlternativeName(
                [
                    x509.DNSName("Db.Example.Com"),
                    x509.IPAddress(ip_address("127.0.0.2")),
                ]
            ),
            critical=False,
        )
        .sign(ca_key, hashes.SHA256())
    )
    certificate = crypto.X509.from_cryptography(leaf_certificate)
    assert _verify_server_hostname(certificate, b"db.example.com")
    assert _verify_server_hostname(certificate, b"127.0.0.2")
    assert not _verify_server_hostname(certificate, b"wrong.example.com")
    assert not _verify_server_hostname(certificate, b"127.0.0.1")


def test_query_completed_and_truncated() -> None:
    completed_connection = FakeConnection(FakeCursor())
    completed = asyncio.run(
        client(completed_connection)
        .query()
        .invoke(call("query"), QueryRowsInput("SELECT @p1 AS value", (17,)))
    )
    assert completed.branch == "completed"
    assert completed.value.rows == ({"value": "17"},)
    assert not completed_connection.rolled_back and completed_connection.closed
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
    rejected_error = tds_base.DatabaseError("secret value")
    rejected_error.number = 208
    rejected_error.state = 1
    rejected_error.severity = 16
    rejected_connection = FakeConnection(FakeCursor(failure=rejected_error, fail_on="SELECT"))
    rejected = asyncio.run(
        client(rejected_connection)
        .query()
        .invoke(call("query"), QueryRowsInput("SELECT @p1 AS value", (17,)))
    )
    assert rejected.branch == "providerRejected"
    assert rejected.failure.kind == FailureKind.NOT_FOUND
    assert "secret value" not in rejected.failure.message

    retry_error = tds_base.DatabaseError("deadlock details")
    retry_error.number = 1205
    retry_error.state = 1
    retry_error.severity = 13
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
    assert not bounded_connection.rolled_back
    assert not bounded_connection.committed


def test_execute_non_output_reply_read_budget_prevents_commit() -> None:
    class ReplyTransport:
        def recv_into(self, buffer: bytearray, _length: int = 0) -> int:
            buffer[:] = b"x" * len(buffer)
            return len(buffer)

    class LargeReplyCursor(FakeCursor):
        def __init__(self) -> None:
            super().__init__(description=(), rowcount=1)
            self._session = SimpleNamespace(
                process_end=lambda _marker: None,
                done_flags=0,
                rows_affected=1,
                _reader=SimpleNamespace(_transport=ReplyTransport()),
            )

        def nextset(self) -> bool:
            reply = bytearray(64 << 10)
            self._session._reader._transport.recv_into(reply)
            self._session._reader._transport.recv_into(reply)
            return False

    connection = FakeConnection(LargeReplyCursor())
    result = asyncio.run(
        client(connection, max_response_bytes=1024)
        .execute()
        .invoke(call("execute"), ExecuteStatementInput("UPDATE dbo.t SET x = @p1", (1,)))
    )
    assert result.branch == "limitExceeded"
    assert result.failure.kind == FailureKind.RESPONSE_TOO_LARGE
    assert not connection.committed


def test_query_retry_and_execute_rejection_invalid_response_and_defect() -> None:
    retry_error = tds_base.DatabaseError("provider details")
    retry_error.number = 1205
    retry_error.state = 1
    retry_error.severity = 13
    retry_connection = FakeConnection(FakeCursor(failure=retry_error, fail_on="SELECT"))
    retry = asyncio.run(
        client(retry_connection).query().invoke(call("query"), QueryRowsInput("SELECT @p1 AS value", (17,)))
    )
    assert retry.kind == "retry"

    rejected_error = tds_base.DatabaseError("provider details")
    rejected_error.number = 2627
    rejected_error.state = 1
    rejected_error.severity = 16
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
