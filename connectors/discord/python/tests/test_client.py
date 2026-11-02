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

from dex_connectors.discord import (
    AddReactionInput,
    Client,
    Config,
    CreateMessageInput,
    Credentials,
    GetChannelInput,
)

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


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("emoji", "expected_emoji_path"),
    [
        (
            "👩‍👩‍👧‍👦",
            b"%F0%9F%91%A9%E2%80%8D%F0%9F%91%A9%E2%80%8D%F0%9F%91%A7%E2%80%8D%F0%9F%91%A6",
        ),
        (
            "🏴\U000e0067\U000e0062\U000e0065\U000e006e\U000e0067\U000e007f",
            b"%F0%9F%8F%B4%F3%A0%81%A7%F3%A0%81%A2%F3%A0%81%A5%F3%A0%81%AE%F3%A0%81%A7%F3%A0%81%BF",
        ),
        ("\U0001faea", b"%F0%9F%AB%AA"),
        ("party_blob:123456789012345678", b"party_blob:123456789012345678"),
    ],
)
async def test_reaction_emoji_matches_go_path_escape(emoji: str, expected_emoji_path: bytes) -> None:
    """Accept joined and tagged emoji and preserve the custom emoji separator."""
    channel_id = "112233445566778899"
    message_id = "998877665544332211"
    requests: list[httpx.Request] = []

    def reply(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(204)

    async with httpx.AsyncClient(transport=httpx.MockTransport(reply)) as transport:
        client = Client(Config(endpoint="http://127.0.0.1:23970"), CREDENTIALS, transport)
        call = Call(CallID("reaction-emoji"), REFERENCE, OperationRef("discord", "addReaction"))
        attempt = await client.add_reaction().invoke(call, AddReactionInput(channel_id, message_id, emoji))

    assert attempt.branch == "added"
    assert len(requests) == 1
    assert requests[0].url.raw_path == (
        b"/channels/112233445566778899/messages/998877665544332211/reactions/"
        + expected_emoji_path
        + b"/@me"
    )
