# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Slack Socket Mode WebSocket connections for the Slack Triggers."""

from __future__ import annotations

import asyncio
import json
from datetime import timedelta
from typing import Any, Final, Protocol, cast

from websockets.asyncio.client import ClientConnection, connect
from websockets.exceptions import WebSocketException
from websockets.frames import Opcode
from websockets.protocol import Event

DEFAULT_MAXIMUM_SOCKET_MESSAGE_BYTES: Final = 4_194_304
"""Default largest Socket Mode message in bytes, the Slack client's default response limit."""
_HANDSHAKE_TIMEOUT_SECONDS: Final = 30.0


class SlackSocketConnection(Protocol):
    """One open Slack Socket Mode connection."""

    async def read_message(self, read_timeout: timedelta) -> Any:
        """Return the next decoded JSON message.

        Raise when the connection ends, a message is not JSON text, or neither a message nor a ping
        arrives for read_timeout; Slack pings idle connections, and every ping extends the wait.
        """
        ...

    async def write_message(self, value: Any) -> None:
        """Send one JSON message."""
        ...

    async def close(self) -> None:
        """Close the connection; closing twice is harmless."""
        ...


class SlackSocketDialer(Protocol):
    """Opens a Socket Mode URL; errors must not contain the URL, which carries a connection ticket."""

    async def __call__(self, url: str) -> SlackSocketConnection:
        """Return the open connection."""
        ...


class SocketModeConnectionError(Exception):
    """A Socket Mode connection failure whose message never names the Socket Mode URL."""


def redact_socket_url(text: str, url: str) -> str:
    """Remove the Socket Mode URL, whose connection ticket must not reach a record, from text."""
    return text.replace(url, "[Socket Mode URL]") if url else text


class _PingObservingConnection(ClientConnection):
    """Records the arrival of every ping, which websockets answers with a pong itself."""

    def __init__(self, *arguments: Any, **keywords: Any) -> None:
        super().__init__(*arguments, **keywords)
        self.ping_received = asyncio.Event()

    def process_event(self, event: Event) -> None:
        super().process_event(event)
        if getattr(event, "opcode", None) is Opcode.PING:
            self.ping_received.set()


class _WebSocketConnection:
    def __init__(self, connection: _PingObservingConnection, url: str) -> None:
        self._connection = connection
        self._url = url

    async def read_message(self, read_timeout: timedelta) -> Any:
        receiving = asyncio.ensure_future(self._connection.recv())
        try:
            while True:
                self._connection.ping_received.clear()
                pinged = asyncio.ensure_future(self._connection.ping_received.wait())
                try:
                    done, _ = await asyncio.wait(
                        {receiving, pinged},
                        timeout=read_timeout.total_seconds(),
                        return_when=asyncio.FIRST_COMPLETED,
                    )
                finally:
                    pinged.cancel()
                if receiving in done:
                    break
                if not done:
                    await self.close()
                    raise SocketModeConnectionError("read Slack Socket Mode envelope: read timeout")
        except BaseException:
            receiving.cancel()
            raise
        try:
            message = receiving.result()
        except WebSocketException as error:
            raise SocketModeConnectionError(
                f"read Slack Socket Mode envelope: {redact_socket_url(str(error), self._url)}"
            ) from None
        if not isinstance(message, str):
            raise SocketModeConnectionError("read Slack Socket Mode envelope: binary message")
        try:
            return json.loads(message)
        except ValueError:
            raise SocketModeConnectionError("read Slack Socket Mode envelope: invalid JSON") from None

    async def write_message(self, value: Any) -> None:
        try:
            await self._connection.send(json.dumps(value, separators=(",", ":")))
        except WebSocketException:
            raise SocketModeConnectionError("write Slack Socket Mode message failed") from None

    async def close(self) -> None:
        try:
            await self._connection.close()
        except (OSError, WebSocketException):
            return  # The connection is already gone, which is what close asks for.


async def dial_slack_socket(
    url: str, maximum_message_bytes: int = DEFAULT_MAXIMUM_SOCKET_MESSAGE_BYTES
) -> SlackSocketConnection:
    """Dial a Socket Mode URL with the websockets package.

    websockets answers every ping with a pong. A message larger than maximum_message_bytes, a positive
    integer, ends the connection. Its bounded receive buffer stops reading the socket while received
    envelopes wait, so a reader blocked on a full delivery queue stops pulling from Slack. Raises
    SocketModeConnectionError, whose message never contains the URL.
    """
    if isinstance(maximum_message_bytes, bool) or maximum_message_bytes < 1:
        raise ValueError("Slack Socket Mode message limit must be a positive integer")
    try:
        connection = await connect(
            url,
            create_connection=_PingObservingConnection,
            open_timeout=_HANDSHAKE_TIMEOUT_SECONDS,
            # Slack pings every connection; the runner's read timeout detects a dead one.
            ping_interval=None,
            max_size=maximum_message_bytes,
            compression=None,
        )
    except (OSError, TimeoutError, WebSocketException) as error:
        raise SocketModeConnectionError(
            f"connect Slack Socket Mode: {redact_socket_url(str(error), url)}"
        ) from None
    return _WebSocketConnection(cast(_PingObservingConnection, connection), url)
