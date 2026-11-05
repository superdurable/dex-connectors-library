# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Focused Slack transport, configuration, and provider shape checks."""

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    OperationRef,
    SecretString,
    StaticCredentialProvider,
)

from dex_connectors.slack import Client, Config, Credentials
from dex_connectors.slack._generated.connection import decode_credentials, decode_shared_credentials
from dex_connectors.slack.client import _decode_body

REFERENCE = ConnectionRef("slack", "workspace")
CREDENTIALS = Credentials(
    "",
    SecretString(""),
    SecretString("bot-token"),
    SecretString(""),
    SecretString("user-token"),
    SecretString(""),
    SecretString(""),
)


def test_configuration_and_credential_decoding() -> None:
    """Reject unsafe endpoints and undeclared stored secret fields."""
    provider = StaticCredentialProvider({REFERENCE: CREDENTIALS})
    with pytest.raises(ValueError):
        Client(Config(endpoint="http://api.slack.com/api"), provider)
    with pytest.raises(ValueError):
        decode_credentials({"bot_token": "bot", "user_token": "user", "unknown": "secret"})
    assert decode_shared_credentials({"bot_token": "bot", "user_token": "user"}).user_token.reveal() == "user"


@pytest.mark.parametrize(
    "body",
    [
        b'{"ok":"true"}',
        b'{"messages":[{"ts":42}]}',
        b'{"message":[]}',
        b'{"response_metadata":{"next_cursor":17}}',
    ],
)
def test_wrong_typed_provider_fields_fail(body: bytes) -> None:
    """Provider schema mismatches must not silently become empty messages."""
    with pytest.raises(ValueError):
        _decode_body(body)


@pytest.mark.asyncio
async def test_query_escape_matches_go_values_encoding() -> None:
    """Encode tilde and spaces exactly like Go's url.Values.Encode."""
    captured: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        captured.append(request)
        return httpx.Response(200, json={"ok": True, "messages": []})

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as transport:
        client = Client(Config(), StaticCredentialProvider({REFERENCE: CREDENTIALS}), transport)
        call = Call(
            CallID("7e327e25-7e56-5ddd-8be8-3df0838a289c"),
            REFERENCE,
            OperationRef("slack", "listThreadMessages"),
        )
        response = await client._request(
            call,
            CREDENTIALS,
            "user_token",
            "GET",
            "conversations.replies",
            query={"ts": "a~b", "channel": "C 1"},
        )
    assert response.status == 200
    assert captured[0].url.query == b"channel=C+1&ts=a%7Eb"


@pytest.mark.asyncio
async def test_client_closes_shared_oauth_transport() -> None:
    """The generated project connection shares one transport for calls and refresh."""
    transport = httpx.AsyncClient(transport=httpx.MockTransport(lambda _: httpx.Response(200)))
    client = Client(Config(), StaticCredentialProvider({REFERENCE: CREDENTIALS}), transport)
    await client.aclose()
    assert transport.is_closed
