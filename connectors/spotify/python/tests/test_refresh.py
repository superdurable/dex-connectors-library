# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check generated Spotify OAuth refresh against Go agreement cases."""

import base64
import json
from datetime import UTC, datetime
from pathlib import Path
from urllib.parse import parse_qs

import httpx
import pytest
from dex_connectors.sdk import CredentialRefreshState
from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver

from dex_connectors.spotify import Credentials
from dex_connectors.spotify._generated.connection import decode_credentials, encode_credentials
from dex_connectors.spotify._generated.refresh import AUTHORIZATION, CREDENTIAL_FIELDS


@pytest.mark.asyncio
async def test_generated_confidential_refresh_agrees_with_go() -> None:
    """Verify exact token request and rotated secret-safe credential material."""
    cases = json.loads(Path(__file__).with_name("refresh_agreement_cases.json").read_text())
    case = cases[0]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, json=case["response"])

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        driver = ManifestCredentialRefreshDriver[Credentials](
            "Spotify", AUTHORIZATION, CREDENTIAL_FIELDS, http_client
        )
        prior = decode_credentials(case["credentials"])
        result = await driver.refresh(CredentialRefreshState(prior, None, datetime.now(UTC)))
    assert len(requests) == 1
    assert str(requests[0].url) == case["endpoint"]
    assert parse_qs(requests[0].content.decode()) == {
        key: [value] for key, value in case["parameters"].items()
    }
    basic = base64.b64encode(f"{case['clientId']}:{case['clientSecret']}".encode()).decode()
    assert requests[0].headers["Authorization"] == f"Basic {basic}"
    encoded = encode_credentials(result.credentials)
    assert encoded["access_token"] == case["updates"]["access_token"]
    assert encoded["refresh_token"] == case["updates"]["refresh_token"]


@pytest.mark.asyncio
async def test_generated_pkce_refresh_uses_public_client_id() -> None:
    """Refresh without a client secret through Spotify's PKCE token form."""
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(
            200,
            json={
                "access_token": "replacement",
                "refresh_token": "rotated",
                "expires_in": 3600,
                "scope": "playlist-read-private",
                "token_type": "Bearer",
            },
        )

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        driver = ManifestCredentialRefreshDriver[Credentials](
            "Spotify", AUTHORIZATION, CREDENTIAL_FIELDS, http_client
        )
        prior = decode_credentials(
            {"oauth_client_id": "client-id", "access_token": "prior", "refresh_token": "refresh"}
        )
        result = await driver.refresh(CredentialRefreshState(prior, None, datetime.now(UTC)))
    assert len(requests) == 1
    assert "Authorization" not in requests[0].headers
    assert parse_qs(requests[0].content.decode()) == {
        "grant_type": ["refresh_token"],
        "refresh_token": ["refresh"],
        "client_id": ["client-id"],
    }
    assert result.credentials.access_token.reveal() == "replacement"
