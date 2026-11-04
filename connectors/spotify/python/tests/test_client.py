# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Focused Spotify configuration and provider-shape checks."""

import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.spotify import Client, Config, Credentials
from dex_connectors.spotify.client import _decode_page


@pytest.fixture
def credentials() -> StaticCredentialProvider[Credentials]:
    """Supply inert call-time credentials."""
    reference = ConnectionRef("spotify", "playlist-reader")
    return StaticCredentialProvider(
        {reference: Credentials("", SecretString(""), SecretString("inert"), SecretString(""))}
    )


def test_client_rejects_remote_http_and_applies_timeout(
    credentials: StaticCredentialProvider[Credentials],
) -> None:
    """Preserve Go's endpoint policy and the manifest timeout."""
    with pytest.raises(ValueError):
        Client(Config(endpoint="http://api.spotify.test/v1"), credentials)
    client = Client(Config(endpoint="http://127.0.0.1/v1", timeout_seconds=3), credentials)
    assert client.config.timeout_seconds == 3


@pytest.mark.parametrize(
    "body",
    [
        b'{"limit":2,"offset":0,"total":0,"items":null}',
        b'{"limit":"2","offset":0,"total":0,"items":[]}',
        b'{"limit":2,"offset":0,"total":0,"items":[{"item":{"type":"track","id":123}}]}',
    ],
)
def test_invalid_provider_page_fails(body: bytes) -> None:
    """Wrong provider shapes never become a successful empty page."""
    with pytest.raises((ValueError, TypeError)):
        _decode_page(body, "3cEYpjA9oz9GiPac4AsH4n")


def test_generated_project_configuration_and_private_credentials() -> None:
    """Retain the declared timeout and reject unexpected stored secret fields."""
    from dex_connectors.spotify._generated.connection import _decode_config, decode_credentials

    config = _decode_config(
        {"endpoint": "https://api.spotify.com/v1", "timeout": 3_000_000_000, "maxResponseBytes": 4096}
    )
    assert config.timeout_seconds == 3
    assert config.max_response_bytes == 4096
    assert decode_credentials({"access_token": "inert"}).access_token.reveal() == "inert"
    with pytest.raises(ValueError):
        decode_credentials({"access_token": "inert", "unrecognized": "secret"})


@pytest.mark.asyncio
async def test_client_closes_injected_oauth_transport(
    credentials: StaticCredentialProvider[Credentials],
) -> None:
    """Generated project connections close their shared provider and refresh transport."""
    import httpx

    transport = httpx.AsyncClient(transport=httpx.MockTransport(lambda _: httpx.Response(200)))
    client = Client(Config(), credentials, transport)
    await client.aclose()
    assert transport.is_closed
