# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""LinkedIn client validation and generated OAuth configuration checks."""

import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.linkedin import Client, Config, Credentials
from dex_connectors.linkedin._generated.connection import _decode_config, decode_credentials
from dex_connectors.linkedin.client import _verified_email


@pytest.fixture
def credentials() -> StaticCredentialProvider[Credentials]:
    """Supply inert OAuth material for client construction."""
    reference = ConnectionRef("linkedin", "signup")
    return StaticCredentialProvider(
        {reference: Credentials("", SecretString(""), SecretString("inert"), SecretString(""))}
    )


def test_client_address_policy_and_timeout(credentials: StaticCredentialProvider[Credentials]) -> None:
    """Require HTTPS outside loopback and honor the manifest timeout."""
    with pytest.raises(ValueError):
        Client(Config(user_info_url="http://api.linkedin.test/v2/userinfo"), credentials)
    client = Client(Config(user_info_url="http://127.0.0.1/v2/userinfo", timeout_seconds=3), credentials)
    assert client.config.timeout_seconds == 3


def test_project_config_and_credentials() -> None:
    """Decode all connection settings and reject extra private fields."""
    config = _decode_config(
        {
            "userInfoUrl": "https://api.linkedin.com/v2/userinfo",
            "timeout": 3_000_000_000,
            "maxResponseBytes": 4096,
        }
    )
    assert config.timeout_seconds == 3 and config.max_response_bytes == 4096
    assert decode_credentials({"access_token": "inert"}).access_token.reveal() == "inert"
    with pytest.raises(ValueError):
        decode_credentials({"access_token": "inert", "unrecognized": "secret"})


def test_verified_email_requires_address_and_claim() -> None:
    """Reject unverified claims and display names in provider email fields."""
    assert _verified_email("Ada@Example.COM", True) == "ada@example.com"
    assert _verified_email("Ada <ada@example.com>", True) == ""
    assert _verified_email("ada@example.com", False) == ""


@pytest.mark.asyncio
async def test_client_closes_injected_oauth_transport(
    credentials: StaticCredentialProvider[Credentials],
) -> None:
    """Close the shared provider and refresh HTTP transport."""
    transport = httpx.AsyncClient(transport=httpx.MockTransport(lambda _: httpx.Response(200)))
    client = Client(Config(), credentials, transport)
    await client.aclose()
    assert transport.is_closed
