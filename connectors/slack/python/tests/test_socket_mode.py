# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The websockets dialer against a local server: pings extend the read timeout, and silence ends it."""

from __future__ import annotations

import asyncio
import json
from datetime import timedelta

import pytest
from websockets.asyncio.server import ServerConnection, serve

from dex_connectors.slack import dial_slack_socket
from dex_connectors.slack.socket_mode import SocketModeConnectionError

READ_TIMEOUT = timedelta(milliseconds=150)


async def test_pings_keep_a_quiet_connection_readable_past_several_read_timeouts() -> None:
    acknowledged: list[str] = []

    async def pinging_server(connection: ServerConnection) -> None:
        for _ in range(12):
            await connection.ping(b"keepalive")
            await asyncio.sleep(0.05)
        await connection.send(json.dumps({"envelope_id": "e1", "type": "events_api", "payload": {}}))
        acknowledged.append(json.loads(await connection.recv())["envelope_id"])
        await connection.wait_closed()

    async with serve(pinging_server, "127.0.0.1", 0) as server:
        port = server.sockets[0].getsockname()[1]
        socket = await dial_slack_socket(f"ws://127.0.0.1:{port}/link/?ticket=SENTINEL")
        try:
            message = await socket.read_message(READ_TIMEOUT)
            assert message["envelope_id"] == "e1"
            await socket.write_message({"envelope_id": "e1"})
            for _ in range(100):
                if acknowledged:
                    break
                await asyncio.sleep(0.01)
            assert acknowledged == ["e1"]
        finally:
            await socket.close()
            await socket.close()


async def test_silence_and_invalid_messages_end_the_connection_without_the_url() -> None:
    async def silent_then_binary(connection: ServerConnection) -> None:
        await asyncio.sleep(0.4)
        await connection.send(b"\x00")
        await connection.wait_closed()

    async with serve(silent_then_binary, "127.0.0.1", 0) as server:
        port = server.sockets[0].getsockname()[1]
        url = f"ws://127.0.0.1:{port}/link/?ticket=SENTINEL"
        socket = await dial_slack_socket(url)
        with pytest.raises(SocketModeConnectionError, match="read timeout"):
            await socket.read_message(READ_TIMEOUT)
        await socket.close()
        socket = await dial_slack_socket(url)
        with pytest.raises(SocketModeConnectionError, match="binary message"):
            await socket.read_message(timedelta(seconds=5))
        await socket.close()
    with pytest.raises(SocketModeConnectionError) as refused:
        await dial_slack_socket(url)
    assert "SENTINEL" not in str(refused.value)
