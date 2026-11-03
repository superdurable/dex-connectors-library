# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Go parity checks for native-driver classification and statement safety."""

import socket
import ssl
import struct
from datetime import timedelta
from decimal import Decimal
from uuid import uuid4

import pytest
from dex_connectors.sdk import Call, CallID, ConnectionRef, FailureKind, OperationRef
from mysql.connector.connection import MySQLConnection
from mysql.connector.constants import ClientFlag, FieldFlag, FieldType
from mysql.connector.errors import DatabaseError

from dex_connectors.mysql import Client, Config, QueryRowsInput
from dex_connectors.mysql.client import (
    _classify,
    _column_type_name,
    _commit_transaction,
    _CommitNotSent,
    _decode_value,
    _ExactValueConnection,
    _ExactValueProtocol,
    _execute_prepared,
    _StrictTLSConnection,
    _TLSUnavailable,
    _validate_statement,
)


@pytest.mark.parametrize(
    ("number", "state", "branch", "kind"),
    [
        (1213, "40001", "retry", FailureKind.CONFLICT),
        (1205, "HY000", "retry", FailureKind.CONFLICT),
        (1040, "08004", "retry", FailureKind.AVAILABILITY),
        (1053, "08S01", "retry", FailureKind.AVAILABILITY),
        (1927, "70100", "retry", FailureKind.AVAILABILITY),
        (4031, "HY000", "retry", FailureKind.AVAILABILITY),
        (1615, "HY000", "retry", FailureKind.AVAILABILITY),
        (1158, "08S01", "retry", FailureKind.AVAILABILITY),
        (1045, "28000", "providerRejected", FailureKind.AUTHENTICATION),
        (3159, "HY000", "providerRejected", FailureKind.AUTHENTICATION),
        (1862, "HY000", "providerRejected", FailureKind.AUTHENTICATION),
        (1044, "42000", "providerRejected", FailureKind.AUTHORIZATION),
        (1142, "42000", "providerRejected", FailureKind.AUTHORIZATION),
        (1792, "25006", "providerRejected", FailureKind.AUTHORIZATION),
        (1290, "HY000", "providerRejected", FailureKind.AUTHORIZATION),
        (1049, "42000", "providerRejected", FailureKind.NOT_FOUND),
        (1146, "42S02", "providerRejected", FailureKind.NOT_FOUND),
        (1054, "42S22", "providerRejected", FailureKind.NOT_FOUND),
        (1062, "23000", "providerRejected", FailureKind.CONFLICT),
        (1452, "23000", "providerRejected", FailureKind.CONFLICT),
        (3819, "HY000", "providerRejected", FailureKind.CONFLICT),
        (4025, "23000", "providerRejected", FailureKind.CONFLICT),
        (3024, "HY000", "providerRejected", FailureKind.AVAILABILITY),
        (1969, "70100", "providerRejected", FailureKind.AVAILABILITY),
        (1317, "70100", "providerRejected", FailureKind.AVAILABILITY),
        (1226, "42000", "providerRejected", FailureKind.QUOTA_EXHAUSTED),
        (1064, "42000", "providerRejected", FailureKind.VALIDATION),
        (1366, "HY000", "providerRejected", FailureKind.VALIDATION),
        (1406, "22001", "providerRejected", FailureKind.VALIDATION),
        (1235, "42000", "providerRejected", FailureKind.VALIDATION),
        (1105, "HY000", "providerRejected", FailureKind.PROVIDER_REJECTION),
    ],
)
def test_server_error_classification(number: int, state: str, branch: str, kind: FailureKind) -> None:
    error = DatabaseError("secret provider response", errno=number, sqlstate=state)
    classified = _classify(error, "statement")
    assert (classified.branch, classified.kind) == (branch, kind)
    assert "secret" not in classified.message
    assert classified.number == number
    assert classified.sql_state == state


def test_commit_classification_matches_definite_rollback_and_unknown_commit() -> None:
    rollback = _classify(DatabaseError("secret", errno=1213, sqlstate="40001"), "commit")
    assert (rollback.branch, rollback.kind) == ("retry", FailureKind.CONFLICT)
    uncertain = _classify(DatabaseError("secret", errno=1105, sqlstate="HY000"), "commit")
    assert (uncertain.branch, uncertain.kind) == ("uncertain", FailureKind.AVAILABILITY)
    network = _classify(ConnectionResetError(), "commit")
    assert (network.branch, network.kind) == ("uncertain", FailureKind.TRANSPORT)


def test_local_refusal_retries_and_unknown_dns_is_terminal() -> None:
    refusal = _classify(ConnectionRefusedError(), "connect")
    assert (refusal.branch, refusal.kind) == ("retry", FailureKind.AVAILABILITY)
    missing = _classify(socket.gaierror(socket.EAI_NONAME, "secret"), "connect")
    assert (missing.branch, missing.kind) == ("providerRejected", FailureKind.NOT_FOUND)


@pytest.mark.parametrize("statement", ["CREATE TABLE t (id INT)", "LOCK TABLES t WRITE", "", "  "])
def test_unsafe_read_statements_are_rejected(statement: str) -> None:
    with pytest.raises(ValueError):
        _validate_statement(statement, (), frozenset({"SELECT", "WITH"}))


def test_parameter_binding_keeps_user_values_out_of_sql() -> None:
    assert _validate_statement(
        "SELECT id FROM t WHERE name = ?", ("a'; DROP TABLE t",), frozenset({"SELECT"})
    ) == ("a'; DROP TABLE t",)


def test_prepared_statement_sends_original_sql_and_rejects_missing_parameters() -> None:
    class Connection:
        def __init__(self) -> None:
            self.prepared_sql = b""
            self.executions = 0

        def cmd_stmt_prepare(self, statement: bytes, **kwargs: object) -> dict[str, object]:
            self.prepared_sql = statement
            return {"statement_id": 1, "parameters": [object()]}

        def cmd_stmt_execute(self, *args: object, **kwargs: object) -> dict[str, object]:
            self.executions += 1
            return {}

    class Cursor:
        def __init__(self, connection: Connection) -> None:
            self._connection = connection
            self._read_timeout = None
            self._write_timeout = None
            self._prepared = None
            self._executed = None

        def _handle_result(self, result: object) -> None:
            pass

    connection = Connection()
    cursor = Cursor(connection)
    statement = "SELECT id FROM t WHERE id = ? -- don't change 100%s or it\\'s"
    with pytest.raises(Exception, match="parameter count") as error:
        _execute_prepared(cursor, statement, ())
    assert _classify(error.value, "statement").branch == "defect"
    assert connection.prepared_sql == statement.encode()
    assert connection.executions == 0
    _execute_prepared(Cursor(connection), statement, (1,))
    assert connection.executions == 1


def test_driver_connect_errors_retry_or_reject_like_go() -> None:
    refused = _classify(DatabaseError("secret", errno=2003, sqlstate="HY000"), "connect")
    assert (refused.branch, refused.kind) == ("retry", FailureKind.AVAILABILITY)
    missing = _classify(DatabaseError("secret", errno=2005, sqlstate="HY000"), "connect")
    assert (missing.branch, missing.kind) == ("providerRejected", FailureKind.NOT_FOUND)
    lost = _classify(DatabaseError("secret", errno=2013, sqlstate="HY000"), "statement")
    assert (lost.branch, lost.kind) == ("retry", FailureKind.TRANSPORT)


def test_wrapped_dns_failure_is_not_retried() -> None:
    try:
        raise socket.gaierror(socket.EAI_NONAME, "secret")
    except socket.gaierror as cause:
        error = DatabaseError("secret", errno=2003, sqlstate="HY000")
        error.__cause__ = cause
    classified = _classify(error, "connect")
    assert (classified.branch, classified.kind) == ("providerRejected", FailureKind.NOT_FOUND)
    assert "secret" not in classified.message


def test_tls_modes_use_supported_driver_option(monkeypatch: pytest.MonkeyPatch) -> None:
    import dex_connectors.mysql.client as client_module

    captured: list[dict[str, object]] = []
    monkeypatch.setattr(client_module, "_StrictTLSConnection", lambda **options: captured.append(options))
    for mode in ("required", "verify-ca", "verify-identity"):
        Client(Config("localhost", "app", "dex_app", ssl_mode=mode), object())._open("password")
        assert captured[-1]["tls_versions"] == ["TLSv1.2", "TLSv1.3"]
        assert "ssl_tls_versions" not in captured[-1]


def test_binary_year_and_signed_time_preserve_wire_values() -> None:
    protocol = _ExactValueProtocol()
    year_field = ("year", FieldType.YEAR, None, None, None, None, None, 0, 63)
    assert protocol._parse_binary_values([year_field], b"\x00" + struct.pack("<H", 2024)) == (2024,)
    negative_time = b"\x0c\x01" + struct.pack("<I", 0) + b"\x0c\x22\x38" + struct.pack("<I", 123457)
    assert protocol._parse_binary_time(negative_time)[1] == -timedelta(
        hours=12, minutes=34, seconds=56, microseconds=123457
    )
    negative_millisecond = b"\x0c\x01" + struct.pack("<I", 0) + b"\x00\x00\x00" + struct.pack("<I", 1000)
    assert _decode_value("TIME", protocol._parse_binary_time(negative_millisecond)[1]) == "-00:00:00.001"
    assert (
        _decode_value("TIME", -timedelta(hours=12, minutes=34, seconds=56, microseconds=500000), 2)
        == "-12:34:56.50"
    )
    assert _decode_value("TIME", timedelta(), 6) == "00:00:00.000000"


def test_binary_zero_and_partial_dates_preserve_mysql_text() -> None:
    protocol = _ExactValueProtocol()
    assert protocol._parse_binary_timestamp(b"\x00", FieldType.DATE)[1] == "0000-00-00"
    assert protocol._parse_binary_timestamp(b"\x00", FieldType.DATETIME)[1] == "0000-00-00 00:00:00"
    assert (
        _decode_value("DATETIME", protocol._parse_binary_timestamp(b"\x00", FieldType.DATETIME)[1], 6)
        == "0000-00-00 00:00:00.000000"
    )
    partial = b"\x04" + struct.pack("<HBB", 2024, 0, 0)
    assert protocol._parse_binary_timestamp(partial, FieldType.DATE)[1] == "2024-00-00"
    invalid_day = b"\x04" + struct.pack("<HBB", 2024, 2, 31)
    assert protocol._parse_binary_timestamp(invalid_day, FieldType.DATE)[1] == "2024-02-31"
    assert (
        _decode_value("TIMESTAMP", protocol._parse_binary_timestamp(b"\x00", FieldType.TIMESTAMP)[1])
        == "0000-00-00 00:00:00"
    )
    year_zero = b"\x07" + struct.pack("<HBBBBB", 0, 1, 1, 0, 0, 0)
    assert (
        _decode_value("DATETIME", protocol._parse_binary_timestamp(year_zero, FieldType.DATETIME)[1])
        == "0000-01-01T00:00:00"
    )
    year_zero_fraction = b"\x0b" + struct.pack("<HBBBBBI", 0, 2, 29, 1, 2, 3, 500000)
    datetime_value = protocol._parse_binary_timestamp(year_zero_fraction, FieldType.DATETIME)[1]
    timestamp_value = protocol._parse_binary_timestamp(year_zero_fraction, FieldType.TIMESTAMP)[1]
    assert (
        _decode_value("DATETIME", datetime_value, 6) == "0000-02-29T01:02:03.5"
    )
    assert _decode_value("TIMESTAMP", timestamp_value, 6) == "0000-02-29T01:02:03.5Z"


def test_column_metadata_keeps_time_fraction_digits() -> None:
    strings = ("def", "db", "table", "table", "clock", "clock")
    packet = b"\x00" * 4 + b"".join(bytes((len(value),)) + value.encode() for value in strings)
    packet += struct.pack("<BHIBHBxx", 12, 45, 11, FieldType.TIME, 0, 2)
    assert _ExactValueProtocol.parse_column(packet)[5] == 2


def test_decimal_scale_is_preserved() -> None:
    assert _decode_value("DECIMAL", Decimal("0.00000000")) == "0.00000000"
    assert _decode_value("DECIMAL", Decimal("0.00000001")) == "0.00000001"
    assert _decode_value("DECIMAL", Decimal("-0.0000001")) == "-0.0000001"


def test_float_and_large_time_use_exact_decimal_output() -> None:
    widened = struct.unpack("<f", struct.pack("<f", 0.1))[0]
    assert _decode_value("FLOAT", widened) == 0.1
    duration = timedelta(days=24, seconds=83589, microseconds=742888)
    assert _decode_value("TIME", duration) == "599:13:09.742888"


@pytest.mark.parametrize(
    ("bits", "expected"),
    [
        (0x39800000, 0.00024414063),
        (0xB9800000, -0.00024414063),
        (0x0F800000, 1.2621775e-29),
        (0x6B000000, 1.5474251e26),
        (0x6C800000, 1.2379401e27),
        (0x7F7FFFFF, 3.4028235e38),
    ],
)
def test_float32_matches_go_shortest_form(bits: int, expected: float) -> None:
    value = struct.unpack("<f", struct.pack("<I", bits))[0]
    assert _decode_value("FLOAT", value) == expected


def test_non_verification_tls_handshake_failure_retries() -> None:
    error = DatabaseError("secret", errno=2055)
    error.__cause__ = ssl.SSLEOFError("connection closed")
    assert _classify(error, "connect").branch == "retry"


def test_verified_tls_rejects_server_without_ssl_capability() -> None:
    capabilities = (
        ClientFlag.PROTOCOL_41 | ClientFlag.SECURE_CONNECTION | ClientFlag.PLUGIN_AUTH
    ) & ~ClientFlag.SSL
    handshake = (
        b"\x0a8.4.0\x00"
        + struct.pack("<I", 7)
        + b"abcdefgh\x00"
        + struct.pack("<H", capabilities & 0xFFFF)
        + b"\xff"
        + struct.pack("<H", 2)
        + struct.pack("<H", capabilities >> 16)
        + b"\x15"
        + bytes(10)
        + b"ijklmnopqrst\x00caching_sha2_password\x00"
    )
    packet = bytearray(struct.pack("<I", len(handshake))[:3] + b"\x00" + handshake)

    class Socket:
        def recv(self, *args: object, **kwargs: object) -> bytearray:
            return packet

    connection = _StrictTLSConnection()
    connection._socket = Socket()
    connection._protocol = _ExactValueProtocol()
    connection._ssl = {"verify_cert": True}
    connection._ssl_disabled = False
    with pytest.raises(_TLSUnavailable) as error:
        connection._do_handshake()
    disposition = _classify(error.value, "connect")
    assert (disposition.branch, disposition.kind) == (
        "providerRejected",
        FailureKind.PROVIDER_REJECTION,
    )


@pytest.mark.parametrize(
    ("max_rows", "max_response_bytes", "expected_rows"),
    [(1, 1024, ({"number": "0"},)), (1000, 2, ())],
)
def test_truncated_prepared_query_leaves_unread_rows_on_discarded_connection(
    monkeypatch: pytest.MonkeyPatch,
    max_rows: int,
    max_response_bytes: int,
    expected_rows: tuple[dict[str, str], ...],
) -> None:
    def packet(body: bytes) -> bytearray:
        return bytearray(struct.pack("<I", len(body))[:3] + b"\x00" + body)

    def column(name: bytes) -> bytearray:
        names = (b"def", b"db", b"table", b"table", name, name)
        return packet(
            b"".join(bytes((len(value),)) + value for value in names)
            + b"\x0c"
            + struct.pack("<HIBHB", 63, 20, FieldType.LONGLONG, 0, 0)
            + b"\x00\x00"
        )

    eof = packet(b"\xfe" + struct.pack("<HH", 0, 2))
    responses = [
        packet(b"\x00\x00\x00" + struct.pack("<HH", 2, 0)),
        packet(b"\x00" + struct.pack("<IHHBH", 1, 1, 0, 0, 0)),
        column(b"number"),
        eof,
        packet(b"\x01"),
        column(b"number"),
        eof,
        *[packet(b"\x00\x00" + struct.pack("<q", number)) for number in range(3)],
        eof,
    ]

    class Socket:
        def __init__(self) -> None:
            self.packets = responses
            self.sock = self
            self.sent: list[bytes] = []

        def recv(self, *args: object, **kwargs: object) -> bytearray:
            return self.packets.pop(0)

        def send(self, payload: bytes, *args: object, **kwargs: object) -> int:
            self.sent.append(bytes(payload))
            return len(payload)

        def sendall(self, payload: bytes, *args: object, **kwargs: object) -> None:
            self.sent.append(bytes(payload))

        def shutdown(self) -> None:
            pass

        def close_connection(self) -> None:
            pass

    fake_socket = Socket()
    connection = _ExactValueConnection()
    connection._protocol = _ExactValueProtocol()
    connection._socket = fake_socket
    monkeypatch.setattr(connection, "is_connected", lambda: True)
    client = Client(
        Config(
            "127.0.0.1",
            "app",
            "database",
            ssl_mode="disabled",
            max_rows=max_rows,
            max_response_bytes=max_response_bytes,
        ),
        object(),
    )
    monkeypatch.setattr(client, "_open", lambda password: connection)
    monkeypatch.setattr(client, "_session", lambda opened: ("1", "8.4.0"))
    call_id = CallID(str(uuid4()))
    call = Call(
        call_id, ConnectionRef("mysql", "live"), OperationRef("mysql", "query"), idempotency_key=call_id
    )

    result = client.query()._invoke_sync(call, QueryRowsInput("SELECT number FROM t"), (), "password")

    assert result.branch == "truncated"
    assert result.value is not None and result.value.rows == expected_rows
    assert any(command[:1] == b"\x17" for command in fake_socket.sent)
    assert not any(command[:1] == b"\x19" for command in fake_socket.sent)


def test_binary_collation_is_text_unless_charset_is_binary() -> None:
    assert _column_type_name(FieldType.VAR_STRING, FieldFlag.BINARY, 46) == "VARCHAR"
    assert _column_type_name(FieldType.BLOB, FieldFlag.BINARY, 46) == "TEXT"
    assert _column_type_name(FieldType.VAR_STRING, FieldFlag.BINARY, 63) == "VARBINARY"
    assert _column_type_name(FieldType.BLOB, FieldFlag.BINARY, 63) == "BLOB"


def test_json_rejects_numbers_the_json_codec_would_change() -> None:
    assert _decode_value("JSON", '{"ok":true,"count":2,"fraction":1.25}') == {
        "ok": True,
        "count": 2,
        "fraction": 1.25,
    }
    for payload in ('{"n":1.10}', '{"n":12345678901234567890.123456789}', '{"n":9007199254740992}'):
        with pytest.raises(ValueError):
            _decode_value("JSON", payload)


@pytest.mark.parametrize(
    "statement",
    ["(SELECT 1) UNION (SELECT 2)", "--\nSELECT 1", "--\x01comment\nSELECT 1", "#comment\nSELECT 1"],
)
def test_leading_keyword_accepts_go_comment_and_parenthesis_forms(statement: str) -> None:
    assert _validate_statement(statement, (), frozenset({"SELECT"})) == ()


@pytest.mark.parametrize("statement", ["/*!SELECT 1*/", "/*M!SELECT 1*/"])
def test_executable_comments_remain_rejected(statement: str) -> None:
    with pytest.raises(ValueError):
        _validate_statement(statement, (), frozenset({"SELECT"}))


def test_tls_required_rejects_a_server_without_tls_before_authentication(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    def handshake(connection: MySQLConnection) -> None:
        connection._handshake = {"capabilities": 0}

    monkeypatch.setattr(MySQLConnection, "_do_handshake", handshake)
    connection = _StrictTLSConnection()
    with pytest.raises(_TLSUnavailable):
        connection._do_handshake()


def test_commit_retries_only_when_no_bytes_were_sent() -> None:
    class Socket:
        def __init__(self, written: int) -> None:
            self.written = written

        def send(self, payload: memoryview) -> int:
            if not self.written:
                raise ConnectionResetError()
            return min(self.written, len(payload))

    class Connection:
        def __init__(self, written: int) -> None:
            self._socket = type("Transport", (), {"sock": Socket(written)})()

        def commit(self) -> None:
            self._socket.sock.sendall(b"COMMIT")
            raise ConnectionResetError()

    with pytest.raises(_CommitNotSent):
        _commit_transaction(Connection(0))
    with pytest.raises(ConnectionResetError):
        _commit_transaction(Connection(6))
