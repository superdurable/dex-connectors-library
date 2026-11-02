# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest refresh contracts against a fake provider token endpoint."""

import base64
import json
import pickle
from copy import deepcopy
from dataclasses import asdict, dataclass, field
from datetime import UTC, datetime, timedelta
from typing import Any
from urllib.parse import parse_qs

import httpx
import pytest
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import padding, rsa

from dex_connectors.sdk import CredentialRefreshState, ReauthorizationRequiredError, SecretString
from dex_connectors.sdk.oauthtoken import (
    ClientAuthenticationMethod,
    ClientCredentials,
    ManifestCredentialRefreshDriver,
    TokenEndpoint,
)


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


@pytest.mark.asyncio
async def test_manifest_static_method_never_refreshes() -> None:
    """A method without a declared grant stays static even with old expiry."""
    credentials = Credentials(SecretString("old"), SecretString("prior"), auth_method_id="static")
    def reject_request(_: httpx.Request) -> httpx.Response:
        raise AssertionError("static method must not call the token endpoint")

    async with httpx.AsyncClient(transport=httpx.MockTransport(reject_request)) as client:
        driver = ManifestCredentialRefreshDriver[Credentials]("Example", AUTH, FIELDS, client)
        now = datetime.now(UTC)
        state = CredentialRefreshState(credentials, now - timedelta(hours=1), now)
        assert not driver.refresh_required(state)
        with pytest.raises(ReauthorizationRequiredError):
            await driver.refresh(state)


@pytest.mark.asyncio
async def test_manifest_multi_method_credentials_require_method_identity() -> None:
    """A consuming credential dataclass must expose the manifest method ID."""
    @dataclass(frozen=True)
    class WithoutMethodID:
        access_token: SecretString

    async with httpx.AsyncClient() as client:
        driver = ManifestCredentialRefreshDriver[WithoutMethodID]("Example", AUTH, FIELDS, client)
        state = CredentialRefreshState(
            WithoutMethodID(SecretString("old")), None, datetime.now(UTC)
        )
        with pytest.raises(ValueError, match="auth_method_id"):
            driver.refresh_required(state)


@pytest.mark.asyncio
async def test_manifest_missing_expiry_follows_declaration_not_grant_count() -> None:
    """A non-expiring GitHub-style token remains usable without refresh material."""
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, json={"access_token": "replacement", "token_type": "Bearer"})

    auth: dict[str, Any] = deepcopy(AUTH)
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        driver = ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, client)
        state = CredentialRefreshState(
            Credentials(SecretString("non-expiring"), SecretString("")), None, datetime.now(UTC)
        )
        assert not driver.refresh_required(state)
        assert requests == []

        auth["methods"][0]["oauth2"]["refresh"]["missingExpiry"] = {"lifetime": "2h"}
        lifetime_driver = ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, client)
        assert lifetime_driver.refresh_required(state)


@pytest.mark.asyncio
async def test_manifest_missing_token_type_requires_declaration() -> None:
    """Atlassian-style responses can omit token_type when explicitly allowed."""
    auth: dict[str, Any] = deepcopy(AUTH)

    def respond(_: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200, json={"access_token": "next", "refresh_token": "rotated", "expires_in": 3600}
        )

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        state = CredentialRefreshState(
            Credentials(SecretString("old"), SecretString("prior")), None, datetime.now(UTC)
        )
        strict = ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, client)
        with pytest.raises(ValueError):
            await strict.refresh(state)
        auth["methods"][0]["oauth2"]["refresh"]["acceptsMissingTokenType"] = True
        allowed = ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, client)
        result = await allowed.refresh(state)
        assert result.credentials.refresh_token.reveal() == "rotated"


@pytest.mark.asyncio
async def test_manifest_form_post_secret_and_optional_public_client() -> None:
    """Required secrets fail closed; optional empty secrets use public-client form encoding."""
    auth: dict[str, Any] = deepcopy(AUTH)
    method = auth["methods"][0]
    method["oauth2"]["tokenRequestEncoding"] = "form"
    method["oauth2"]["tokenEndpointAuthMethod"] = "client_secret_post"
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, json={"access_token": "next", "expires_in": 3600, "token_type": "Bearer"})

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        driver = ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, client)
        prior = Credentials(SecretString("old"), SecretString("prior"))
        state = CredentialRefreshState(prior, None, datetime.now(UTC))
        await driver.refresh(state)
        assert requests[-1].content == (
            b"client_id=client-id&client_secret=client-secret&grant_type=refresh_token"
            b"&refresh_token=prior&scope=offline_access+files.read"
        )
        assert requests[-1].headers["content-type"] == "application/x-www-form-urlencoded"

        empty_secret = Credentials(SecretString("old"), SecretString("prior"), client_secret=SecretString(""))
        empty_state = CredentialRefreshState(empty_secret, None, datetime.now(UTC))
        method["fields"][3]["required"] = True
        required_driver = ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, client)
        with pytest.raises(ReauthorizationRequiredError):
            await required_driver.refresh(empty_state)
        assert len(requests) == 1

        method["fields"][3]["required"] = False
        public_driver = ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, client)
        await public_driver.refresh(empty_state)
        assert requests[-1].content == (
            b"client_id=client-id&grant_type=refresh_token&refresh_token=prior"
            b"&scope=offline_access+files.read"
        )


@pytest.mark.asyncio
async def test_manifest_template_uses_go_path_segment_escaping() -> None:
    """Value-source templates preserve the reserved characters Go PathEscape allows."""
    auth: dict[str, Any] = deepcopy(AUTH)
    auth["methods"][0]["oauth2"]["tokenEndpoint"] = {
        "template": "https://oauth.example.test/{client_id}/token"
    }
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, json={
            "access_token": "next", "expires_in": 3600, "token_type": "Bearer",
        })

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        driver = ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, client)
        state = CredentialRefreshState(
            Credentials(
                SecretString("old"), SecretString("prior"),
                client_id="a:b@c&d=e+f$/g;h,i?j",
            ), None, datetime.now(UTC),
        )
        await driver.refresh(state)
    assert str(requests[0].url) == (
        "https://oauth.example.test/a:b@c&d=e+f$%2Fg%3Bh%2Ci%3Fj/token"
    )


@pytest.mark.asyncio
async def test_manifest_client_credentials_preserves_offline_access_scope() -> None:
    """Client-credentials requests keep every declared scope, including offline_access."""
    auth = {
        "type": "clientCredentials",
        "fields": [
            {"name": "access_token", "type": "secretString"},
            {"name": "client_id", "type": "string"},
            {"name": "client_secret", "type": "secretString"},
        ],
        "clientCredentials": {
            "tokenEndpoint": "https://oauth.example.test/token",
            "clientIDCredential": "client_id", "clientSecretCredential": "client_secret",
            "scopes": ["files.read", "offline_access"],
            "credentialMappings": [{"credential": "access_token", "source": "access_token"}],
        },
    }
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, json={
            "access_token": "next", "expires_in": 3600,
            "token_type": "Bearer", "scope": "files.read offline_access",
        })

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        driver = ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, client)
        state = CredentialRefreshState(
            Credentials(SecretString(""), SecretString("")), None, datetime.now(UTC)
        )
        result = await driver.refresh(state)
    assert result.credentials.access_token.reveal() == "next"
    assert b"scope=files.read+offline_access" in requests[0].content


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "error_code", "is_terminal"),
    [
        (400, "invalid_grant", True),
        (200, "invalid_grant", True),
        (401, "invalid_client", True),
        (500, "invalid_grant", False),
        (400, "invalid_request", False),
        (400, "unauthorized_client", False),
        (400, "unsupported_grant_type", False),
        (400, "invalid_scope", False),
        (400, "access_denied", False),
        (400, "interaction_required", False),
        (400, "consent_required", False),
        (400, "temporarily_unavailable", False),
        (400, "server_error", False),
    ],
)
async def test_manifest_standard_error_code_matrix(
    status: int, error_code: str, is_terminal: bool
) -> None:
    """Only declared errors below HTTP 500 require reauthorization."""

    def respond(_: httpx.Request) -> httpx.Response:
        return httpx.Response(status, json={"error": error_code})

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        driver = ManifestCredentialRefreshDriver[Credentials]("Example", AUTH, FIELDS, client)
        state = CredentialRefreshState(
            Credentials(SecretString("old"), SecretString("prior")), None, datetime.now(UTC)
        )
        with pytest.raises(ReauthorizationRequiredError if is_terminal else OSError):
            await driver.refresh(state)


@pytest.mark.asyncio
async def test_token_response_keeps_mapped_strings_redacted() -> None:
    """Nested mapping values cannot leak through dataclass or pickle serialization."""

    def respond(_: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json={
            "access_token": "secret-access", "refresh_token": "secret-refresh",
            "expires_in": 3600, "token_type": "Bearer", "nested": {"instance": "secret-nested"},
        })

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        endpoint = TokenEndpoint("Example", "https://oauth.example.test/token", client)
        token = await endpoint.exchange_refresh_token(
            ClientCredentials(
                "client-id", SecretString("client-secret"),
                ClientAuthenticationMethod.CLIENT_SECRET_POST,
            ),
            SecretString("prior"),
        )
    assert token.mapped_response_string("nested.instance") == "secret-nested"
    assert all(isinstance(value, SecretString) for value in token._response_fields.values())
    assert "secret-access" not in repr(token._response_fields)
    with pytest.raises(TypeError):
        asdict(token)
    with pytest.raises(TypeError):
        pickle.dumps(token)


@dataclass(frozen=True, slots=True)
class MultiGrantCredentials:
    user_access: SecretString
    user_refresh: SecretString
    bot_access: SecretString
    bot_refresh: SecretString
    client_id: str = "client-id"
    client_secret: SecretString = field(default_factory=lambda: SecretString("client-secret"))
    auth_method_id: str = "oauth"


@pytest.mark.asyncio
async def test_manifest_multi_grant_rotation_and_missing_expiry() -> None:
    """Both Slack-style grants rotate, while a missing expiry follows the declaration."""
    auth: dict[str, Any] = {
        "methods": [{
            "id": "oauth", "type": "oauth2",
            "fields": [
                {"name": name, "type": "secretString"}
                for name in ("user_access", "user_refresh", "bot_access", "bot_refresh", "client_secret")
            ] + [{"name": "client_id", "type": "string"}],
            "oauth2": {
                "tokenEndpoint": "https://oauth.example.test/token",
                "clientIDCredential": "client_id", "clientSecretCredential": "client_secret",
                "scopes": ["chat.write"],
                "refresh": {"grants": [
                    {
                        "refreshTokenCredential": "user_refresh",
                        "credentialMappings": [
                            {"credential": "user_access", "source": "access_token"},
                            {"credential": "user_refresh", "source": "refresh_token"},
                        ],
                    },
                    {
                        "refreshTokenCredential": "bot_refresh",
                        "credentialMappings": [
                            {"credential": "bot_access", "source": "access_token"},
                            {"credential": "bot_refresh", "source": "refresh_token"},
                        ],
                    },
                ]},
            },
        }],
    }
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        prior = parse_qs(request.content.decode())["refresh_token"][0]
        return httpx.Response(200, json={
            "access_token": f"{prior}-access", "refresh_token": f"{prior}-rotated",
            "expires_in": 3600, "token_type": "Bearer", "scope": "chat.write",
        })

    credentials = MultiGrantCredentials(
        SecretString("old-user"), SecretString("user-token"),
        SecretString("old-bot"), SecretString("bot-token"),
    )
    fields = {name: name for name in (
        "user_access", "user_refresh", "bot_access", "bot_refresh", "client_id", "client_secret",
    )}
    now = datetime.now(UTC)
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        driver = ManifestCredentialRefreshDriver[MultiGrantCredentials]("Example", auth, fields, client)
        assert not driver.refresh_required(CredentialRefreshState(credentials, None, now))
        result = await driver.refresh(CredentialRefreshState(credentials, now, now))
        assert result.credentials.user_refresh.reveal() == "user-token-rotated"
        assert result.credentials.bot_refresh.reveal() == "bot-token-rotated"
        assert len(requests) == 2
        auth["methods"][0]["oauth2"]["refresh"]["missingExpiry"] = {"lifetime": "2h"}
        lifetime_driver = ManifestCredentialRefreshDriver[MultiGrantCredentials](
            "Example", auth, fields, client
        )
        assert lifetime_driver.refresh_required(CredentialRefreshState(credentials, None, now))


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
                "scopes": ["mail.read", "offline_access"],
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
                "scope": "mail.read offline_access",
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
    assertion = parse_qs(requests[0].content.decode())["assertion"][0]
    header, claims, signature = assertion.split(".")
    decoded_header = json.loads(base64.urlsafe_b64decode(header + "=="))
    decoded_claims = json.loads(base64.urlsafe_b64decode(claims + "=="))
    private_key.public_key().verify(
        base64.urlsafe_b64decode(signature + "=="),
        f"{header}.{claims}".encode(), padding.PKCS1v15(), hashes.SHA256(),
    )
    assert decoded_header == {"alg": "RS256", "typ": "JWT"}
    assert decoded_claims["iss"] == "service@example.test"
    assert decoded_claims["sub"] == "user@example.test"
    assert decoded_claims["aud"] == "https://oauth.example.test/token"
    assert decoded_claims["scope"] == "mail.read offline_access"
    assert decoded_claims["exp"] - decoded_claims["iat"] == 3600
