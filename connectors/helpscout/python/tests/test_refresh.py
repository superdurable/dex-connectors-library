# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay generated Help Scout OAuth refresh agreement cases."""

import json
from datetime import UTC, datetime
from pathlib import Path
from urllib.parse import parse_qs

import httpx
import pytest
from dex_connectors.sdk import CredentialRefreshState
from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver

from dex_connectors.helpscout import Credentials
from dex_connectors.helpscout._generated.connection import decode_credentials, encode_credentials
from dex_connectors.helpscout._generated.refresh import AUTHORIZATION, CREDENTIAL_FIELDS

CASES = json.loads(Path(__file__).with_name("refresh_agreement_cases.json").read_text())


@pytest.mark.parametrize("case", CASES, ids=lambda case: case["name"])
async def test_generated_refresh_agrees_with_go(case: dict[str, object]) -> None:
    """Check exact grant form, token policy, and persisted credential updates."""
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, json=case["response"])

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        driver = ManifestCredentialRefreshDriver[Credentials](
            "Help Scout", AUTHORIZATION, CREDENTIAL_FIELDS, http_client
        )
        state = CredentialRefreshState(decode_credentials(case["credentials"]), None, datetime.now(UTC))
        if case.get("expectedError"):
            with pytest.raises(ValueError):
                await driver.refresh(state)
        else:
            result = await driver.refresh(state)
            encoded = encode_credentials(result.credentials)
            for field, value in case["updates"].items():
                assert encoded[field] == value
    assert len(requests) == 1
    assert str(requests[0].url) == case["endpoint"]
    assert parse_qs(requests[0].content.decode()) == {
        key: [value] for key, value in case["parameters"].items()
    }
    assert "Authorization" not in requests[0].headers
