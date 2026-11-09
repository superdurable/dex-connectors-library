# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Client settings select the Creatio Identity Service token endpoint."""

from datetime import UTC, datetime
from urllib.parse import parse_qs

import httpx
from dex_connectors.sdk import CredentialRefreshState, SecretString, StaticCredentialProvider

from dex_connectors.creatio import Client, Config, Credentials


async def test_client_mints_at_configured_identity_service_endpoint() -> None:
    """Send the client credentials grant to the instance-specific URL."""
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(
            200, json={"access_token": "next-access", "token_type": "Bearer", "expires_in": 3600}
        )

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        client = Client(
            Config(base_url="https://app.example.com", token_endpoint="https://identity.example.com/connect/token"),
            StaticCredentialProvider({}),
            http_client,
        )
        state = CredentialRefreshState(
            Credentials("sample-client-id", SecretString("sample-client-secret"), SecretString("")),
            None,
            datetime.now(UTC),
        )
        result = await client.refresh_driver.refresh(state)
        assert result.credentials.access_token.reveal() == "next-access"
        assert len(requests) == 1
        assert str(requests[0].url) == "https://identity.example.com/connect/token"
        parameters = parse_qs(requests[0].read().decode())
        assert parameters == {
            "grant_type": ["client_credentials"],
            "client_id": ["sample-client-id"],
            "client_secret": ["sample-client-secret"],
        }
