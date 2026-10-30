# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""OAuth token endpoint, expiry, and JWT-bearer verification."""

import base64
import json
from datetime import UTC, datetime, timedelta

import httpx
import pytest
from cryptography.hazmat.primitives.asymmetric import rsa

from dex_connectors.sdk import ReauthorizationRequiredError, SecretString
from dex_connectors.sdk.oauthtoken import (
    ClientAuthenticationMethod,
    ClientCredentials,
    JWTBearerAssertion,
    MissingExpiryRule,
    TokenEndpoint,
    forced_refresh_floor,
    has_all_scopes,
    has_exact_scopes,
    is_refresh_required,
    proportional_refresh_skew,
    sign_jwt_bearer_assertion,
)


def test_refresh_timing_and_scope_rules() -> None:
    """Apply proportional skew, missing-expiry policy, and exact scope sets."""
    now = datetime(2026, 10, 29, tzinfo=UTC)
    expiry = now + timedelta(minutes=1)
    assert proportional_refresh_skew(now, expiry) == timedelta(seconds=15)
    assert forced_refresh_floor(now, expiry) == timedelta(seconds=15)
    assert is_refresh_required(
        True, expiry, now + timedelta(seconds=46), MissingExpiryRule.REFRESH_WHEN_EXPIRY_MISSING, now
    )
    assert not is_refresh_required(True, expiry, now, MissingExpiryRule.REFRESH_WHEN_EXPIRY_MISSING, now)
    assert is_refresh_required(True, None, now, MissingExpiryRule.REFRESH_WHEN_EXPIRY_MISSING)
    assert not is_refresh_required(True, None, now, MissingExpiryRule.KEEP_WHEN_EXPIRY_MISSING)
    assert has_all_scopes("read,write extra", ("read", "write"))
    assert not has_exact_scopes("read,write extra", ("read", "write"))
    assert has_exact_scopes("read,write", ("write", "read"))


@pytest.mark.asyncio
async def test_refresh_exchange_and_failure_classification() -> None:
    """Bound token responses, prevent redirects, and classify terminal grants."""
    requests: list[httpx.Request] = []
    status = 200
    payload: dict[str, object] = {
        "access_token": "INERT_ACCESS", "expires_in": 3600, "token_type": "Bearer"
    }

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(status, json=payload)

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        endpoint = TokenEndpoint(
            "Example", "https://oauth.example/token", client,
            terminal_error_codes=("invalid_grant",),
        )
        credentials = ClientCredentials(
            "client", SecretString("INERT_CLIENT_SECRET"), ClientAuthenticationMethod.CLIENT_SECRET_POST
        )
        result = await endpoint.exchange_refresh_token(credentials, SecretString("INERT_REFRESH"))
        assert result.access_token.reveal() == "INERT_ACCESS"
        assert result.next_refresh_token(SecretString("prior")).reveal() == "prior"
        assert b"INERT_REFRESH" in requests[0].content
        assert "INERT_ACCESS" not in repr(result)
        status = 400
        payload = {"error": "invalid_grant", "error_description": "INERT_SENSITIVE_DETAIL"}
        with pytest.raises(ReauthorizationRequiredError) as terminal:
            await endpoint.exchange_refresh_token(credentials, SecretString("INERT_REFRESH"))
        assert "INERT_SENSITIVE_DETAIL" not in str(terminal.value)
        status = 503
        with pytest.raises(OSError, match="HTTP 503"):
            await endpoint.exchange_refresh_token(credentials, SecretString("INERT_REFRESH"))
        with pytest.raises(ReauthorizationRequiredError):
            await endpoint.exchange_refresh_token(credentials, SecretString(""))
        assert len(requests) == 3


@pytest.mark.asyncio
async def test_token_endpoint_rejects_unsafe_configuration() -> None:
    """Reject plaintext, grant substitution, retained token fields, and redirects."""
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(lambda request: httpx.Response(302))
    ) as client:
        credentials = ClientCredentials(
            "client", SecretString("secret"), ClientAuthenticationMethod.CLIENT_SECRET_BASIC
        )
        insecure = TokenEndpoint("Example", "http://oauth.example/token", client)
        with pytest.raises(ValueError, match="URL"):
            await insecure.exchange_refresh_token(credentials, SecretString("refresh"))
        override = TokenEndpoint(
            "Example", "https://oauth.example/token", client,
            additional_request_parameters={"grant_type": "other"},
        )
        with pytest.raises(ValueError, match="grant"):
            await override.exchange_refresh_token(credentials, SecretString("refresh"))
        retained = TokenEndpoint(
            "Example", "https://oauth.example/token", client,
            retained_response_fields=("access_token",),
        )
        with pytest.raises(ValueError, match="retain"):
            await retained.exchange_refresh_token(credentials, SecretString("refresh"))
        safe = TokenEndpoint("Example", "https://oauth.example/token", client)
        with pytest.raises(OSError, match="HTTP 302"):
            await safe.exchange_refresh_token(credentials, SecretString("refresh"))


def test_jwt_bearer_assertion_signing() -> None:
    """Create a signed assertion with provider claims and redacted rendering."""
    key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    assertion = JWTBearerAssertion(
        "service@example.test", "https://oauth.example/token",
        datetime(2026, 10, 29, tzinfo=UTC), timedelta(hours=1), "user@example.test", "scope",
    )
    signed = sign_jwt_bearer_assertion(assertion, key)
    encoded_claims = signed.reveal().split(".")[1]
    claims = json.loads(base64.urlsafe_b64decode(encoded_claims + "=="))
    assert claims["sub"] == "user@example.test" and claims["scope"] == "scope"
    assert "service@example.test" not in str(signed)
