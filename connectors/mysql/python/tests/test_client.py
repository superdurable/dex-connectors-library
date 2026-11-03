# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Go parity checks for native-driver classification and statement safety."""

import socket

import pytest
from dex_connectors.sdk import FailureKind
from mysql.connector.connection import MySQLConnection
from mysql.connector.errors import DatabaseError

from dex_connectors.mysql.client import (
    _classify,
    _commit_transaction,
    _CommitNotSent,
    _driver_placeholders,
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
