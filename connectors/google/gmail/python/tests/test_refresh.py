# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Google OAuth and Workspace credential exchange without live accounts."""

import base64
import json
from datetime import UTC, datetime, timedelta

import httpx
import pytest
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from dex_connectors.sdk import CredentialRefreshState, ReauthorizationRequiredError, SecretString

from dex_connectors.google_gmail import CredentialRefreshDriver, Credentials

NOW = datetime(2026, 3, 4, 9, 15, tzinfo=UTC)


def oauth_credentials() -> Credentials:
    """Return inert OAuth material with a verified example mailbox."""
    return Credentials(
        SecretString("old-access"),
        "owner@example.org",
        oauth_client_id="client-id",
        oauth_client_secret=SecretString("client-secret"),
        refresh_token=SecretString("old-refresh"),
    )


def test_refresh_decision_uses_five_minute_skew() -> None:
    """Expiring or missing access material requires refresh before the provider call."""
    driver = CredentialRefreshDriver(
        httpx.AsyncClient(transport=httpx.MockTransport(lambda _: httpx.Response(500)))
    )
    credential = oauth_credentials()
    assert not driver.refresh_required(CredentialRefreshState(credential, NOW + timedelta(minutes=6), NOW))
    assert driver.refresh_required(CredentialRefreshState(credential, NOW + timedelta(minutes=4), NOW))
    assert driver.refresh_required(CredentialRefreshState(credential, None, NOW))


@pytest.mark.asyncio
async def test_oauth_refresh_rotates_token_and_validates_scopes() -> None:
    """Client-secret-post exchange preserves mailbox identity and rotates refresh material."""
    seen = []

    def answer(request: httpx.Request) -> httpx.Response:
        seen.append(request)
        assert str(request.url) == "https://oauth2.googleapis.com/token"
        assert b"grant_type=refresh_token" in request.content
        assert b"refresh_token=old-refresh" in request.content
        return httpx.Response(
            200,
            json={
                "access_token": "new-access",
                "refresh_token": "new-refresh",
                "expires_in": 3600,
                "token_type": "Bearer",
                "scope": "https://www.googleapis.com/auth/gmail.readonly https://www.googleapis.com/auth/gmail.send",
            },
        )

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as client:
        driver = CredentialRefreshDriver(client, lambda: NOW)
        result = await driver.refresh(CredentialRefreshState(oauth_credentials(), NOW, NOW))
    assert len(seen) == 1
    assert result.credentials.access_token.reveal() == "new-access"
    assert result.credentials.refresh_token.reveal() == "new-refresh"
    assert result.credentials.primary_email == "owner@example.org"
    assert result.expires_at == NOW + timedelta(hours=1)


@pytest.mark.asyncio
async def test_oauth_refresh_retains_prior_token_when_google_omits_rotation() -> None:
    """An omitted refresh_token never erases the stored grant."""
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(
            lambda _: httpx.Response(
                200,
                json={
                    "access_token": "new-access",
                    "expires_in": 3600,
                    "token_type": "Bearer",
                },
            )
        )
    ) as client:
        result = await CredentialRefreshDriver(client, lambda: NOW).refresh(
            CredentialRefreshState(oauth_credentials(), NOW, NOW)
        )
    assert result.credentials.refresh_token.reveal() == "old-refresh"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "response",
    [
        httpx.Response(400, json={"error": "invalid_grant"}),
        httpx.Response(
            200,
            json={
                "access_token": "new-access",
                "expires_in": 3600,
                "token_type": "Bearer",
                "scope": "https://www.googleapis.com/auth/gmail.readonly",
            },
        ),
    ],
)
async def test_oauth_terminal_grant_or_missing_scope_requires_reauthorization(
    response: httpx.Response,
) -> None:
    """A revoked grant and lost send scope both require new consent."""
    async with httpx.AsyncClient(transport=httpx.MockTransport(lambda _: response)) as client:
        with pytest.raises(ReauthorizationRequiredError):
            await CredentialRefreshDriver(client, lambda: NOW).refresh(
                CredentialRefreshState(oauth_credentials(), NOW, NOW)
            )


@pytest.mark.asyncio
async def test_workspace_refresh_signs_delegated_assertion() -> None:
    """The service-account grant uses the configured mailbox as JWT subject."""
    private_key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    pem = private_key.private_bytes(
        serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()
    ).decode()
    key = json.dumps(
        {
            "client_email": "svc@example.iam.gserviceaccount.com",
            "private_key": pem,
            "token_uri": "https://oauth2.googleapis.com/token",
        }
    )

    def answer(request: httpx.Request) -> httpx.Response:
        encoded = dict(item.split("=", 1) for item in request.content.decode().split("&"))
        from urllib.parse import unquote_plus

        assertion = unquote_plus(encoded["assertion"])
        claims = json.loads(base64.urlsafe_b64decode(assertion.split(".")[1] + "=="))
        assert claims["sub"] == "owner@example.org"
        assert (
            claims["scope"]
            == "https://www.googleapis.com/auth/gmail.readonly https://www.googleapis.com/auth/gmail.send"
        )
        return httpx.Response(
            200, json={"access_token": "delegated-access", "expires_in": 3600, "token_type": "Bearer"}
        )

    credential = Credentials(
        SecretString(""),
        "",
        auth_method_id="workspace-domain-delegation",
        service_account_key=SecretString(key),
        delegated_user="owner@example.org",
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as client:
        result = await CredentialRefreshDriver(client, lambda: NOW).refresh(
            CredentialRefreshState(credential, None, NOW)
        )
    assert result.credentials.access_token.reveal() == "delegated-access"
    assert result.credentials.primary_email == "owner@example.org"


@pytest.mark.asyncio
async def test_workspace_invalid_key_requires_reauthorization() -> None:
    """Malformed service-account material fails before any token request."""
    credential = Credentials(
        SecretString(""),
        "",
        auth_method_id="workspace-domain-delegation",
        service_account_key=SecretString("invalid"),
        delegated_user="owner@example.org",
    )
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(lambda _: pytest.fail("unexpected token request"))
    ) as client:
        with pytest.raises(ReauthorizationRequiredError):
            await CredentialRefreshDriver(client, lambda: NOW).refresh(
                CredentialRefreshState(credential, None, NOW)
            )
