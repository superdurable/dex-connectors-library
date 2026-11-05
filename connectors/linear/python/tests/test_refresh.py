# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify generated Linear OAuth refresh against Go agreement cases."""

import json
from datetime import UTC, datetime
from pathlib import Path
from urllib.parse import parse_qs

import httpx
import pytest
from dex_connectors.sdk import CredentialRefreshState
from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver

from dex_connectors.linear import Credentials
from dex_connectors.linear._generated.connection import decode_credentials, encode_credentials
from dex_connectors.linear._generated.refresh import AUTHORIZATION, CREDENTIAL_FIELDS

CASES = json.loads(Path(__file__).with_name("refresh_agreement_cases.json").read_text())


@pytest.mark.parametrize("case", CASES, ids=lambda case: case["name"])
async def test_generated_oauth_refresh_agrees_with_go(case: dict[str, object]) -> None:
    """Check token form, rotated credentials, and rejected malformed responses."""
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, json=case["response"])

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        driver = ManifestCredentialRefreshDriver[Credentials](
            "Linear", AUTHORIZATION, CREDENTIAL_FIELDS, http_client
        )
        material = dict(case["credentials"])
        material["auth_method"] = case["methodId"]
        prior = decode_credentials(material)
        assert driver.refresh_required(CredentialRefreshState(prior, None, datetime.now(UTC)))
        if case.get("expectedError"):
            with pytest.raises((ValueError, OSError)):
                await driver.refresh(CredentialRefreshState(prior, None, datetime.now(UTC)))
        else:
            result = await driver.refresh(CredentialRefreshState(prior, None, datetime.now(UTC)))
            encoded = encode_credentials(result.credentials)
            for field, expected in case["updates"].items():
                assert encoded[field] == expected
            assert result.expires_at > datetime.now(UTC)
    assert len(requests) == 1
    assert str(requests[0].url) == case["endpoint"]
    assert parse_qs(requests[0].content.decode()) == {
        key: [value] for key, value in case["parameters"].items()
    }
    assert "Authorization" not in requests[0].headers
