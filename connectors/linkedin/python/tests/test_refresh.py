# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check generated LinkedIn OAuth refresh against Go agreement cases."""

import json
from datetime import UTC, datetime
from pathlib import Path
from urllib.parse import parse_qs

import httpx
import pytest
from dex_connectors.sdk import CredentialRefreshState
from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver

from dex_connectors.linkedin import Credentials
from dex_connectors.linkedin._generated.connection import decode_credentials, encode_credentials
from dex_connectors.linkedin._generated.refresh import AUTHORIZATION, CREDENTIAL_FIELDS


@pytest.mark.asyncio
async def test_generated_refresh_agrees_with_go() -> None:
    """Send the manifest-declared form request and rotate stored tokens."""
    case = json.loads(Path(__file__).with_name("refresh_agreement_cases.json").read_text())[0]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, json=case["response"])

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        driver = ManifestCredentialRefreshDriver[Credentials](
            "LinkedIn", AUTHORIZATION, CREDENTIAL_FIELDS, http_client
        )
        prior = decode_credentials(case["credentials"])
        result = await driver.refresh(CredentialRefreshState(prior, None, datetime.now(UTC)))
    assert len(requests) == 1
    assert str(requests[0].url) == case["endpoint"]
    assert parse_qs(requests[0].content.decode()) == {
        key: [value] for key, value in case["parameters"].items()
    }
    encoded = encode_credentials(result.credentials)
    assert encoded["access_token"] == case["updates"]["access_token"]
    assert encoded["refresh_token"] == case["updates"]["refresh_token"]


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "case",
    json.loads(Path(__file__).with_name("refresh_agreement_cases.json").read_text()),
    ids=lambda case: case["name"],
)
async def test_all_generated_refresh_agreement_cases(case: dict[str, object]) -> None:
    """Keep every Go-generated OAuth decision visible to the Python package."""
    from dex_connectors.sdk import ReauthorizationRequiredError

    async with httpx.AsyncClient(
        transport=httpx.MockTransport(lambda _: httpx.Response(200, json=case["response"]))
    ) as http_client:
        driver = ManifestCredentialRefreshDriver[Credentials](
            "LinkedIn", AUTHORIZATION, CREDENTIAL_FIELDS, http_client
        )
        prior = decode_credentials(case["credentials"])
        expected_error = case.get("expectedError")
        if expected_error == "reauthorization":
            with pytest.raises(ReauthorizationRequiredError):
                await driver.refresh(CredentialRefreshState(prior, None, datetime.now(UTC)))
        elif expected_error == "retryable":
            with pytest.raises((ValueError, OSError)):
                await driver.refresh(CredentialRefreshState(prior, None, datetime.now(UTC)))
        else:
            result = await driver.refresh(CredentialRefreshState(prior, None, datetime.now(UTC)))
            assert encode_credentials(result.credentials)["access_token"] == case["updates"]["access_token"]
