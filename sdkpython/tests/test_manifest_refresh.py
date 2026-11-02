# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest refresh contracts against a fake provider token endpoint."""

import json
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta

import httpx
import pytest
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import rsa

from dex_connectors.sdk import CredentialRefreshState, ReauthorizationRequiredError, SecretString
from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver


@dataclass(frozen=True, slots=True)
class Credentials:
    access_token: SecretString
    refresh_token: SecretString
    client_id: str = "client-id"
    client_secret: SecretString = field(default_factory=lambda: SecretString("client-secret"))
    auth_method_id: str = "oauth"
    instance_url: str = "old-instance"


AUTH = {
    "defaultMethod": "oauth",
    "methods": [
        {
            "id": "oauth",
            "type": "oauth2",
            "fields": [
                {"name": "access_token", "type": "secretString"},
                {"name": "refresh_token", "type": "secretString"},
                {"name": "client_id", "type": "string"},
                {"name": "client_secret", "type": "secretString"},
                {"name": "instance_url", "type": "url"},
            ],
            "oauth2": {
                "tokenEndpoint": "https://oauth.example.test/token",
                "tokenEndpointAuthMethod": "client_secret_basic",
                "tokenRequestEncoding": "json",
                "clientIDCredential": "client_id",
                "clientSecretCredential": "client_secret",
                "scopes": ["offline_access", "files.read"],
                "credentialMappings": [
                    {"credential": "access_token", "source": "access_token"},
                    {"credential": "refresh_token", "source": "refresh_token"},
                    {"credential": "instance_url", "source": "instance_url"},
                ],
                "refresh": {
                    "terminalErrorCodes": ["invalid_grant", "invalid_client"],
                    "additionalParameters": {"scope": "offline_access files.read"},
                },
            },
        },
        {"id": "static", "type": "apiKey", "fields": []},
    ],
}
FIELDS = {
    name: name for name in ("access_token", "refresh_token", "client_id", "client_secret", "instance_url")
}


@pytest.mark.asyncio
async def test_manifest_refresh_rotates_and_keeps_complete_material() -> None:
    """The manifest controls request shape, mappings, scope, and refresh rotation."""
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(
            200,
            json={
                "access_token": "next-access",
                "refresh_token": "next-refresh",
                "expires_in": 3600,
                "token_type": "Bearer",
                "scope": "files.read",
                "instance_url": "new-instance",
            },
        )

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        driver = ManifestCredentialRefreshDriver[Credentials]("Example", AUTH, FIELDS, client)
        now = datetime.now(UTC)
        state = CredentialRefreshState(
            Credentials(SecretString("old-access"), SecretString("old-refresh")), now, now
        )
        assert driver.refresh_required(state)
        result = await driver.refresh(state)
    assert result.credentials.access_token.reveal() == "next-access"
    assert result.credentials.refresh_token.reveal() == "next-refresh"
    assert result.credentials.client_secret.reveal() == "client-secret"
    assert result.credentials.instance_url == "new-instance"
    assert now + timedelta(minutes=59) < result.expires_at < now + timedelta(minutes=61)
    assert len(requests) == 1
    assert requests[0].url == "https://oauth.example.test/token"
    assert requests[0].headers["content-type"] == "application/json"
    assert requests[0].headers["authorization"].startswith("Basic ")
    assert '"grant_type":"refresh_token"' in requests[0].content.decode().replace(" ", "")


@pytest.mark.asyncio
async def test_manifest_refresh_preserves_unreturned_refresh_token_and_classifies_errors() -> None:
    """Omitted rotation remains stored; only declared terminal errors require reauthorization."""
    status = 200
    response: dict[str, object] = {
        "access_token": "next-access",
        "expires_in": 3600,
        "token_type": "Bearer",
        "scope": "files.read",
    }

    def respond(_: httpx.Request) -> httpx.Response:
        return httpx.Response(status, json=response)

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        driver = ManifestCredentialRefreshDriver[Credentials]("Example", AUTH, FIELDS, client)
        state = CredentialRefreshState(
            Credentials(SecretString("old"), SecretString("prior")), None, datetime.now(UTC)
        )
        result = await driver.refresh(state)
        assert result.credentials.refresh_token.reveal() == "prior"
        status, response = 400, {"error": "invalid_grant"}
        with pytest.raises(ReauthorizationRequiredError):
            await driver.refresh(state)
        status, response = 400, {"error": "invalid_scope"}
        with pytest.raises(OSError):
            await driver.refresh(state)


def test_manifest_static_method_never_refreshes() -> None:
    """A method without a declared grant stays static even with old expiry."""
    credentials = Credentials(SecretString("old"), SecretString("prior"), auth_method_id="static")
    driver = ManifestCredentialRefreshDriver[Credentials]("Example", AUTH, FIELDS, httpx.AsyncClient())
    now = datetime.now(UTC)
    assert not driver.refresh_required(CredentialRefreshState(credentials, now - timedelta(hours=1), now))


@dataclass(frozen=True, slots=True)
class DelegatedCredentials:
    access_token: SecretString
    service_account_key: SecretString
    delegated_user: str
    primary_email: str = ""


@pytest.mark.asyncio
async def test_manifest_jwt_bearer_resolves_key_sources_and_copies_identity() -> None:
    """A service-account grant signs with the stored key and copies declared identity."""
    private_key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    pem = private_key.private_bytes(
        serialization.Encoding.PEM,
        serialization.PrivateFormat.PKCS8,
        serialization.NoEncryption(),
    ).decode()
    auth = {
        "type": "serviceAccount",
        "fields": [
            {"name": "access_token", "type": "secretString"},
            {"name": "service_account_key", "type": "secretString"},
            {"name": "delegated_user", "type": "string"},
            {"name": "primary_email", "type": "string"},
        ],
        "serviceAccount": {
            "jwtBearer": {
                "keyCredential": "service_account_key",
                "privateKey": {"fromKey": "private_key"},
                "issuer": {"fromKey": "client_email"},
                "subject": {"fromCredential": "delegated_user"},
                "audience": {"fromKey": "token_uri"},
                "tokenEndpoint": {"fromKey": "token_uri"},
                "scopes": ["mail.read"],
                "lifetime": "1h",
                "credentialMappings": [{"credential": "access_token", "source": "access_token"}],
                "credentialCopies": [{"credential": "primary_email", "fromCredential": "delegated_user"}],
            }
        },
    }
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(
            200,
            json={
                "access_token": "delegated-access",
                "expires_in": 3600,
                "token_type": "Bearer",
                "scope": "mail.read",
            },
        )

    credentials = DelegatedCredentials(
        SecretString(""),
        SecretString(
            json.dumps(
                {
                    "private_key": pem,
                    "client_email": "service@example.test",
                    "token_uri": "https://oauth.example.test/token",
                }
            )
        ),
        "user@example.test",
    )
    fields = {
        name: name
        for name in (
            "access_token",
            "service_account_key",
            "delegated_user",
            "primary_email",
        )
    }
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        driver = ManifestCredentialRefreshDriver[DelegatedCredentials]("Example", auth, fields, client)
        result = await driver.refresh(CredentialRefreshState(credentials, None, datetime.now(UTC)))
    assert result.credentials.access_token.reveal() == "delegated-access"
    assert result.credentials.primary_email == "user@example.test"
    assert len(requests) == 1
    assert requests[0].url == "https://oauth.example.test/token"
    assert "assertion=" in requests[0].content.decode()
