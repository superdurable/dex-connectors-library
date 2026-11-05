# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run a manifest-declared OAuth refresh against an inert token endpoint."""

import asyncio
from dataclasses import dataclass
from datetime import UTC, datetime

import httpx

from dex_connectors.sdk import CredentialRefreshState, SecretString
from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver


@dataclass(frozen=True, slots=True)
class ExampleCredentials:
    """Private project credential material for this runnable example."""

    client_id: str
    client_secret: SecretString
    access_token: SecretString
    refresh_token: SecretString


async def main() -> None:
    """Refresh an inert credential using the same declaration a generator emits."""
    auth = {
        "type": "oauth2",
        "fields": [
            {"name": "client_id", "type": "string"},
            {"name": "client_secret", "type": "secretString"},
            {"name": "access_token", "type": "secretString"},
            {"name": "refresh_token", "type": "secretString"},
        ],
        "oauth2": {
            "tokenEndpoint": "https://auth.example.test/token",
            "clientIDCredential": "client_id", "clientSecretCredential": "client_secret",
            "scopes": ["read"],
            "refresh": {"responseScopeField": "scopesArray"},
            "credentialMappings": [
                {"credential": "access_token", "source": "access_token"},
                {"credential": "refresh_token", "source": "refresh_token"},
            ],
        },
    }

    def respond(_: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json={
            "access_token": "INERT_NEW_ACCESS", "refresh_token": "INERT_NEW_REFRESH",
            "expires_in": 3600, "token_type": "Bearer", "scope": "unrelated",
        })

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        driver = ManifestCredentialRefreshDriver[ExampleCredentials](
            "Example", auth, {name: name for name in
                              ("client_id", "client_secret", "access_token", "refresh_token")}, client,
        )
        credentials = ExampleCredentials(
            "INERT_CLIENT", SecretString("INERT_SECRET"),
            SecretString("INERT_OLD_ACCESS"), SecretString("INERT_OLD_REFRESH"),
        )
        result = await driver.refresh(CredentialRefreshState(credentials, None, datetime.now(UTC)))
        assert result.credentials.access_token.reveal() == "INERT_NEW_ACCESS"
        assert result.credentials.refresh_token.reveal() == "INERT_NEW_REFRESH"


if __name__ == "__main__":
    asyncio.run(main())
