# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check generated Atlassian OAuth refresh against Go agreement cases."""

import json
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

import httpx
import pytest
from dex_connectors.sdk import CredentialRefreshState, ReauthorizationRequiredError
from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver

from dex_connectors.atlassian_jira_service_management import Credentials
from dex_connectors.atlassian_jira_service_management._generated.connection import (
    decode_credentials,
    encode_credentials,
)
from dex_connectors.atlassian_jira_service_management._generated.refresh import (
    AUTHORIZATION,
    CREDENTIAL_FIELDS,
)

CASES = json.loads(Path(__file__).with_name("refresh_agreement_cases.json").read_text())


@pytest.mark.parametrize("case", CASES, ids=lambda case: case["name"])
async def test_generated_refresh_agrees_with_go(case: dict[str, Any]) -> None:
    """Check grant encoding, scope policy, token type, and rotated credentials."""
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, json=case["response"])

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        driver = ManifestCredentialRefreshDriver[Credentials](
            "Jira Service Management", AUTHORIZATION, CREDENTIAL_FIELDS, http_client
        )
        previous = decode_credentials(case["credentials"])
        state = CredentialRefreshState(previous, None, datetime.now(UTC))
        expected_error = case.get("expectedError")
        if expected_error:
            with pytest.raises(
                ReauthorizationRequiredError if expected_error == "reauthorization" else Exception
            ):
                await driver.refresh(state)
        else:
            result = await driver.refresh(state)
            encoded = encode_credentials(result.credentials)
            for name, value in case["updates"].items():
                assert encoded[name] == value
    assert len(requests) == 1
    assert str(requests[0].url) == case["endpoint"]
    assert json.loads(requests[0].content) == case["parameters"]
    assert "Authorization" not in requests[0].headers
