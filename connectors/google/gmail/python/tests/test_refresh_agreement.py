# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the manifest cases shared with Go's generated refresh agreement."""

import json
from dataclasses import fields
from datetime import UTC, datetime
from pathlib import Path
from typing import Any
from urllib.parse import parse_qs

import httpx
import pytest
from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.hazmat.primitives.serialization import Encoding, NoEncryption, PrivateFormat
from dex_connectors.sdk import CredentialRefreshState, ReauthorizationRequiredError, SecretString

from dex_connectors.google_gmail.project_credentials import decode_credentials
from dex_connectors.google_gmail.refresh import CredentialRefreshDriver

CASES: list[dict[str, Any]] = json.loads(
    (Path(__file__).with_name("refresh_agreement_cases.json")).read_text()
)


def credential_values(credentials: Any) -> dict[str, str]:
    """Read only declared credential values for agreement assertions."""
    values = {}
    for credential_field in fields(credentials):
        if credential_field.name == "auth_method_id":
            continue
        value = getattr(credentials, credential_field.name)
        values[credential_field.name] = value.reveal() if isinstance(value, SecretString) else value
    return values


@pytest.mark.asyncio
@pytest.mark.parametrize("case", CASES, ids=lambda case: case["name"])
async def test_refresh_agrees_with_go_manifest_case(case: dict[str, Any]) -> None:
    """Match Go's sample request, complete credential replacement, and terminal codes."""
    material = dict(case["credentials"])
    material["auth_method"] = case["methodId"]
    if case.get("keyCredential"):
        private_key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
        private_key_pem = private_key.private_bytes(
            Encoding.PEM, PrivateFormat.PKCS8, NoEncryption()
        ).decode()
        key_members = dict(case["keyMembers"])
        key_members[case["privateKeyMember"]] = private_key_pem
        material[case["keyCredential"]] = json.dumps(key_members)
    credentials = decode_credentials(material)
    initial = credential_values(credentials)
    requests: list[httpx.Request] = []
    response_body: dict[str, Any] = case["response"]
    response_status = 200

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(response_status, json=response_body)

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        driver = CredentialRefreshDriver(client)
        state = CredentialRefreshState(credentials, datetime(2000, 1, 1, tzinfo=UTC), datetime.now(UTC))
        result = await driver.refresh(state)
        assert len(requests) == 1
        assert str(requests[0].url) == case["endpoint"]
        if case["encoding"] == "json":
            parameters = json.loads(requests[0].content)
        else:
            parameters = {name: values[0] for name, values in parse_qs(requests[0].content.decode()).items()}
        for name, value in case["parameters"].items():
            assert parameters[name] == value
        for name, value in case["updates"].items():
            initial[name] = value
        assert credential_values(result.credentials) == initial
        assert result.expires_at > state.now

        for terminal_code in case["terminalErrorCodes"]:
            requests.clear()
            response_body = {"error": terminal_code}
            response_status = 400
            with pytest.raises(ReauthorizationRequiredError):
                await driver.refresh(state)
            assert len(requests) == 1
