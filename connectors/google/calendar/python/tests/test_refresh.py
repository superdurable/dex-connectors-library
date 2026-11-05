# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run generated Google OAuth and delegated JWT agreement cases."""

import base64
import json
from datetime import UTC, datetime
from pathlib import Path
from urllib.parse import parse_qs

import httpx
import pytest
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from dex_connectors.sdk import CredentialRefreshState, ReauthorizationRequiredError, RetryError
from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver

from dex_connectors.google_calendar import Credentials
from dex_connectors.google_calendar._generated.connection import decode_credentials, encode_credentials
from dex_connectors.google_calendar._generated.refresh import AUTHORIZATION, CREDENTIAL_FIELDS

CASES = json.loads(Path(__file__).with_name("refresh_agreement_cases.json").read_text())


@pytest.mark.parametrize("case", CASES, ids=lambda case: case["name"])
async def test_generated_refresh_matches_go(case: dict[str, object]) -> None:
    """Check grant requests and Go's accepted or rejected token response."""
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, json=case["response"])

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        driver = ManifestCredentialRefreshDriver[Credentials](
            "Google Calendar", AUTHORIZATION, CREDENTIAL_FIELDS, http_client
        )
        material = dict(case["credentials"])
        material["auth_method"] = case["methodId"]
        if case["methodId"] == "workspace-domain-delegation":
            key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
            key_pem = key.private_bytes(
                serialization.Encoding.PEM,
                serialization.PrivateFormat.PKCS8,
                serialization.NoEncryption(),
            ).decode()
            material[case["keyCredential"]] = json.dumps(
                {**case["keyMembers"], case["privateKeyMember"]: key_pem}
            )
        prior = decode_credentials(material)
        if case.get("expectedError"):
            expected_error = (
                ReauthorizationRequiredError if case["expectedError"] == "reauthorization" else RetryError
            )
            with pytest.raises(expected_error):
                await driver.refresh(CredentialRefreshState(prior, None, datetime.now(UTC)))
        else:
            result = await driver.refresh(CredentialRefreshState(prior, None, datetime.now(UTC)))
            encoded = encode_credentials(result.credentials)
            for key, value in case["updates"].items():
                assert encoded[key] == value
    assert len(requests) == 1
    assert str(requests[0].url) == case["endpoint"]
    actual = parse_qs(requests[0].content.decode())
    for key, value in case["parameters"].items():
        assert actual[key] == [value]
    if case["encoding"] == "jwtBearer":
        assertion = actual["assertion"][0]
        header, claims, signature = assertion.split(".")
        assert header and signature
        decoded = json.loads(base64.urlsafe_b64decode(claims + "=" * (-len(claims) % 4)))
        for case_key, claim_key in (
            ("issuer", "iss"),
            ("subject", "sub"),
            ("audience", "aud"),
            ("scope", "scope"),
        ):
            assert decoded[claim_key] == case["assertion"][case_key]
        assert decoded["exp"] - decoded["iat"] == case["assertion"]["lifetimeSeconds"]
