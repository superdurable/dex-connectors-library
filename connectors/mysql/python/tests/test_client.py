# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Go parity checks for native-driver classification and statement safety."""

import socket
import struct
from datetime import timedelta

import pytest
from dex_connectors.sdk import FailureKind
from mysql.connector.connection import MySQLConnection
from mysql.connector.constants import FieldFlag, FieldType
from mysql.connector.errors import DatabaseError

from dex_connectors.mysql import Client, Config
from dex_connectors.mysql.client import (
    _classify,
    _column_type_name,
    _commit_transaction,
    _CommitNotSent,
    _decode_value,
    _driver_placeholders,
    _ExactValueProtocol,
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
    assert _driver_placeholders("SELECT id FROM t WHERE name = ?") == "SELECT id FROM t WHERE name = %s"
    assert _driver_placeholders("SELECT '?' AS literal, id FROM t WHERE id = ? /* ? */") == (
        "SELECT '?' AS literal, id FROM t WHERE id = %s /* ? */"
    )


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


def test_float_and_large_time_use_exact_decimal_output() -> None:
    widened = struct.unpack("<f", struct.pack("<f", 0.1))[0]
    assert _decode_value("FLOAT", widened) == 0.1
    duration = timedelta(days=24, seconds=83589, microseconds=742888)
    assert _decode_value("TIME", duration) == "599:13:09.742888"


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
