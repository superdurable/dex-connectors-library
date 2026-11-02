# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Focused Discord client validation beyond recorded provider scenarios."""

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    IdempotencyKey,
    OperationRef,
    SecretString,
    StaticCredentialProvider,
)

from dex_connectors.discord import Client, Config, CreateMessageInput, Credentials, GetChannelInput

REFERENCE = ConnectionRef("discord", "discord")
CREDENTIALS = StaticCredentialProvider({REFERENCE: Credentials(SecretString("inert.test.token"))})


def test_client_rejects_non_loopback_http_endpoint() -> None:
    """Disallow bot token transport over nonlocal plaintext HTTP."""
    with pytest.raises(ValueError, match="HTTPS"):
        Client(Config(endpoint="http://discord.example/api/v10"), CREDENTIALS)


@pytest.mark.asyncio
async def test_bad_channel_id_never_reaches_provider() -> None:
    """Validate untrusted path data before resolving the provider request."""

    def unreachable(request: httpx.Request) -> httpx.Response:
        raise AssertionError("invalid channel reached provider")

    async with httpx.AsyncClient(transport=httpx.MockTransport(unreachable)) as transport:
        client = Client(Config(endpoint="http://127.0.0.1:22048"), CREDENTIALS, transport)
        call = Call(CallID("bad-channel"), REFERENCE, OperationRef("discord", "getChannel"))
        attempt = await client.get_channel().invoke(call, GetChannelInput("../invalid"))
        assert attempt.branch == "defect"
        assert attempt.failure is not None
        assert "inert.test.token" not in attempt.failure.message


@pytest.mark.asyncio
async def test_message_timestamp_without_timezone_is_invalid_response() -> None:
    """Reject a provider timestamp that Go's RFC3339 decoder rejects."""
    channel_id = "112233445566778899"

    def reply(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            json={
                "id": "998877665544332211",
                "channel_id": channel_id,
                "author": {"id": "123456789012345678", "username": "dex-bot", "bot": True},
                "content": "hello",
                "timestamp": "2026-10-13T20:00:00",
                "type": 0,
            },
        )

    async with httpx.AsyncClient(transport=httpx.MockTransport(reply)) as transport:
        client = Client(Config(endpoint="http://127.0.0.1:22048"), CREDENTIALS, transport)
        call = Call(
            CallID("bad-timestamp"),
            REFERENCE,
            OperationRef("discord", "createMessage"),
            IdempotencyKey("bad-timestamp"),
        )
        attempt = await client.create_message().invoke(call, CreateMessageInput(channel_id, "hello"))
        assert attempt.branch == "invalidResponse"
