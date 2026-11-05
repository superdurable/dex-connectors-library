# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise identity-result draining through a real python-tds session."""

import asyncio
import socket
import struct
import threading
from uuid import uuid4

import pytest
from dex_connectors.sdk import Call, CallID, ConnectionRef, IdempotencyKey, OperationRef, SecretString

from dex_connectors.microsoft_sql_server import Client, Config, Credentials, ExecuteStatementInput

LOG: list[str] = []


def ucs2(text: str) -> bytes:
    return text.encode("utf-16-le")


def packet(payload: bytes, packet_type: int = 0x04) -> bytes:
    return struct.pack(">BBHHBB", packet_type, 0x01, len(payload) + 8, 0, 1, 0) + payload


def done(status: int, count: int, token: int = 0xFD) -> bytes:
    return struct.pack("<BHHQ", token, status, 0, count)


def envchange_begin() -> bytes:
    body = bytes([8, 8]) + b"\x01\x00\x00\x00\x00\x00\x00\x00" + bytes([0])
    return b"\xe3" + struct.pack("<H", len(body)) + body


def envchange_commit() -> bytes:
    body = bytes([9, 0, 8]) + b"\x01\x00\x00\x00\x00\x00\x00\x00"
    return b"\xe3" + struct.pack("<H", len(body)) + body


def envchange_database(name: str) -> bytes:
    body = bytes([1, len(name)]) + ucs2(name) + bytes([len(name)]) + ucs2(name)
    return b"\xe3" + struct.pack("<H", len(body)) + body


def identity_result() -> bytes:
    collation = b"\x09\x04\xd0\x00\x34"
    columns = struct.pack("<H", 2)
    columns += struct.pack("<IH", 0, 0) + bytes([0x26, 4]) + bytes([0])
    columns += struct.pack("<IH", 0, 0) + bytes([0xE7]) + struct.pack("<H", 256) + collation + bytes([0])
    row = (
        b"\xd1" + bytes([4]) + struct.pack("<i", 57)
        + struct.pack("<H", len(ucs2("16.0.1000.6"))) + ucs2("16.0.1000.6")
    )
    return b"\x81" + columns + row + done(0x10, 1)


def read_message(connection: socket.socket) -> tuple[int, bytes]:
    payload = b""
    while True:
        header = b""
        while len(header) < 8:
            chunk = connection.recv(8 - len(header))
            if not chunk:
                raise EOFError
            header += chunk
        packet_type, status, length = struct.unpack(">BBH", header[:4])
        body = b""
        while len(body) < length - 8:
            chunk = connection.recv(length - 8 - len(body))
            if not chunk:
                raise EOFError
            body += chunk
        payload += body
        if status & 0x01:
            return packet_type, payload


def serve(listener: socket.socket) -> None:
    connection, _ = listener.accept()
    try:
        while True:
            packet_type, payload = read_message(connection)
            if packet_type == 0x12:
                LOG.append("PRELOGIN")
                options = bytes([0]) + struct.pack(">HH", 16, 6) + bytes([1]) + struct.pack(">HH", 22, 1)
                options += bytes([4]) + struct.pack(">HH", 23, 1) + b"\xff"
                options = options.ljust(16, b"\x00")[:16] if len(options) < 16 else options
                prelogin = options + struct.pack(">LH", 0x10000000, 0) + bytes([2]) + bytes([0])
                connection.sendall(packet(prelogin))
            elif packet_type == 0x10:
                LOG.append("LOGIN7")
                name = ucs2("Microsoft SQL Server")
                body = (
                    bytes([1]) + struct.pack(">L", 0x74000004)
                    + bytes([len(name) // 2]) + name + struct.pack(">L", 0x10000000)
                )
                loginack = b"\xad" + struct.pack("<H", len(body)) + body
                connection.sendall(packet(envchange_database("master") + loginack + done(0, 0)))
            elif packet_type == 0x0E:
                request_type = struct.unpack_from("<H", payload, struct.unpack_from("<I", payload, 0)[0])[0]
                LOG.append(f"TRANS {request_type}")
                if request_type == 5:
                    connection.sendall(packet(envchange_begin() + done(0, 0)))
                else:
                    connection.sendall(packet(envchange_commit() + envchange_begin() + done(0, 0)))
            elif packet_type == 0x01:
                text = payload[struct.unpack_from("<I", payload, 0)[0]:].decode("utf-16-le", "replace")
                LOG.append("SQLBATCH " + text[-60:])
                if "@@SPID" in text:
                    connection.sendall(packet(identity_result()))
                else:
                    connection.sendall(packet(done(0, 0)))
            elif packet_type == 0x06:
                LOG.append("ATTENTION")
                connection.sendall(packet(done(0x20, 0)))
            elif packet_type == 0x03:
                LOG.append("RPC")
                connection.sendall(
                    packet(done(0x11, 1, 0xFF) + b"\x79" + struct.pack("<i", 0) + done(0, 0, 0xFE))
                )
            else:
                LOG.append(f"UNKNOWN {packet_type}")
                return
    except EOFError:
        LOG.append("EOF")
    finally:
        connection.close()


class CredentialsSource:
    async def resolve(self, _call: Call) -> Credentials:
        return Credentials(SecretString("test-only-password"))


@pytest.mark.parametrize("max_rows_affected", [None, 1])
def test_identity_done_does_not_count_as_statement_rows(max_rows_affected: int | None) -> None:
    LOG.clear()
    listener = socket.socket()
    for port in range(33500, 33600):
        try:
            listener.bind(("127.0.0.1", port))
            break
        except OSError:
            continue
    else:
        pytest.fail("No SQL Server test port available in 33500-33599")
    listener.listen(1)
    listener.settimeout(5)
    server = threading.Thread(target=serve, args=(listener,), daemon=True)
    server.start()
    client = Client(Config("127.0.0.1", "master", "sa", port, "disable"), CredentialsSource())
    call_id = CallID(str(uuid4()))
    call = Call(
        call_id,
        ConnectionRef("microsoft-sql-server", "probe"),
        OperationRef("microsoft-sql-server", "execute"),
        IdempotencyKey(str(call_id)),
    )
    try:
        outcome = asyncio.run(
            client.execute().invoke(
                call,
                ExecuteStatementInput(
                    "INSERT INTO dbo.t (id) VALUES (@p1)", (1,), max_rows_affected=max_rows_affected
                ),
            )
        )
    finally:
        listener.close()
        server.join(5)
    assert outcome.branch == "completed"
    assert outcome.value.rows_affected == 1
    assert "ATTENTION" not in LOG
