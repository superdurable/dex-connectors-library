# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""FreshBooks refresh repeats the registered callback URI."""

from datetime import UTC, datetime

import httpx
from dex_connectors.sdk import CredentialRefreshState, SecretString, StaticCredentialProvider

from dex_connectors.freshbooks import Client, Config, Credentials


async def test_refresh_sends_redirect_uri_and_rotates_token() -> None:
    """Use the registered HTTPS callback on the provider's JSON refresh grant."""
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(
            200,
            json={
                "access_token": "next-access",
                "refresh_token": "next-refresh",
                "token_type": "Bearer",
                "expires_in": 3600,
                "scope": "user:profile:read user:invoices:read",
            },
        )

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        client = Client(
            Config(redirect_uri="https://dex.example/callback"),
            StaticCredentialProvider({}),
            http_client,
        )
        state = CredentialRefreshState(
            Credentials(
                "client-id",
                SecretString("client-secret"),
                SecretString("old-access"),
                SecretString("old-refresh"),
            ),
            None,
            datetime.now(UTC),
        )
        result = await client.refresh_driver.refresh(state)
        assert result.credentials.access_token.reveal() == "next-access"
        assert result.credentials.refresh_token.reveal() == "next-refresh"
        assert len(requests) == 1
        assert requests[0].url == "https://api.freshbooks.com/auth/oauth/token"
        assert requests[0].headers["Content-Type"] == "application/json"
        assert requests[0].read().decode().find('"redirect_uri":"https://dex.example/callback"') >= 0
