# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest refresh contracts against a fake provider token endpoint."""

import base64
import json
import pickle
from copy import deepcopy
from dataclasses import asdict, dataclass, field, replace
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any
from unittest.mock import patch
from urllib.parse import parse_qs

import httpcore
import httpx
import pytest
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import padding, rsa

from dex_connectors.sdk import CredentialRefreshState, ReauthorizationRequiredError, RetryError, SecretString
from dex_connectors.sdk.oauthtoken import (
    ClientAuthenticationMethod,
    ClientCredentials,
    ManifestCredentialRefreshDriver,
    TokenEndpoint,
)
from dex_connectors.sdk.oauthtoken.manifest_refresh import _read_jwt_expiry_seconds
from dex_connectors.sdk.providerhttp import AddressRefusal


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
async def test_manifest_default_method_scope_and_missing_expiry_match_go() -> None:
    """An empty method uses the default, while scope and expiry follow Go refresh rules."""
    auth: dict[str, Any] = deepcopy(AUTH)
    auth["methods"][0]["oauth2"]["refresh"]["requiredScopes"] = ["files.read"]
    response: dict[str, Any] = {
        "access_token": "next", "expires_in": 3600, "token_type": "Bearer", "scope": "\t  ",
    }
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, json=response)

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        driver = ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, client)
        credentials = Credentials(SecretString("old"), SecretString("prior"), auth_method_id="")
        state = CredentialRefreshState(credentials, None, datetime.now(UTC))
        await driver.refresh(state)
        response["scope"] = "files.read\textra"
        await driver.refresh(state)
        response["scope"] = "files.read,extra"
        with pytest.raises(ReauthorizationRequiredError):
            await driver.refresh(state)
        response = {"access_token": "next", "token_type": "Bearer", "scope": "files.read"}
        with pytest.raises(RetryError, match="required fields"):
            await driver.refresh(state)
    assert len(requests) == 4


@pytest.mark.asyncio
async def test_declared_scope_separators_apply_to_all_and_exact_checks() -> None:
    """Whitespace and space-or-comma declarations match their Go scope checks."""
    auth: dict[str, Any] = deepcopy(AUTH)
    refresh = auth["methods"][0]["oauth2"]["refresh"]
    refresh["requiredScopes"] = ["files.read", "files.write"]
    returned_scope = ""

    def respond(_: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json={
            "access_token": "next", "expires_in": 3600, "token_type": "Bearer", "scope": returned_scope,
        })

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        driver = ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, client)
        credentials = Credentials(SecretString("old"), SecretString("prior"))
        state = CredentialRefreshState(credentials, None, datetime.now(UTC))

        async def check(scope: str, accepted: bool) -> None:
            nonlocal returned_scope
            returned_scope = scope
            if accepted:
                await driver.refresh(state)
            else:
                with pytest.raises(ReauthorizationRequiredError):
                    await driver.refresh(state)

        await check("files.read\tfiles.write", True)
        await check("files.read,files.write", False)
        refresh["scopeSeparators"] = "whitespaceAndComma"
        await check("files.read,files.write", True)
        await check("files.read\tfiles.write", False)
        refresh["exactScopes"] = True
        await check("files.read,files.write", True)
        await check("files.read,files.write,extra", False)
        await check("files.read\tfiles.write", False)
        refresh["scopeSeparators"] = "whitespace"
        await check("files.read\tfiles.write", True)
        await check("files.read,files.write", False)


@pytest.mark.asyncio
async def test_manifest_can_ignore_reported_scope_when_go_driver_does() -> None:
    """An explicit policy accepts a different reported scope."""
    auth: dict[str, Any] = deepcopy(AUTH)
    refresh = auth["methods"][0]["oauth2"]["refresh"]
    response = {"access_token": "next", "expires_in": 3600, "token_type": "Bearer", "scope": "other"}
    transport = httpx.MockTransport(lambda _: httpx.Response(200, json=response))
    async with httpx.AsyncClient(transport=transport) as client:
        credentials = Credentials(SecretString("old"), SecretString("prior"))
        state = CredentialRefreshState(credentials, None, datetime.now(UTC))
        driver = ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, client)
        with pytest.raises(ReauthorizationRequiredError):
            await driver.refresh(state)
        refresh["responseScopes"] = "ignore"
        driver = ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, client)
        assert (await driver.refresh(state)).credentials.access_token.reveal() == "next"


@pytest.mark.asyncio
async def test_schema_decoded_provider_auth_refresh_with_declared_fields() -> None:
    """Real decoded declarations accept Jira's omitted type and Slack's bot/user types."""
    # schema.Decode(connector.yaml) JSON-encodes Spec.Auth; add planned token-type fields.
    fixture_dir = Path(__file__).parent / "fixtures"
    jira_auth = json.loads((fixture_dir / "jira-auth.json").read_text())
    slack_auth = json.loads((fixture_dir / "slack-auth.json").read_text())

    @dataclass(frozen=True)
    class JiraCredentials:
        oauth_client_id: str
        oauth_client_secret: SecretString
        access_token: SecretString
        refresh_token: SecretString

    jira_credentials = JiraCredentials(
        "client", SecretString("secret"), SecretString("old"), SecretString("jira-old")
    )
    jira_fields = {name: name for name in (
        "oauth_client_id", "oauth_client_secret", "access_token", "refresh_token"
    )}

    def jira_response(_: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json={
            "access_token": "jira-new", "refresh_token": "jira-rotated", "expires_in": 3600,
            "scope": "read:jira-work write:jira-work",
        })

    async with httpx.AsyncClient(transport=httpx.MockTransport(jira_response)) as client:
        jira_driver = ManifestCredentialRefreshDriver[JiraCredentials]("Jira", jira_auth, jira_fields, client)
        jira_result = await jira_driver.refresh(
            CredentialRefreshState(jira_credentials, None, datetime.now(UTC))
        )
        assert jira_result.credentials.refresh_token.reveal() == "jira-rotated"

    @dataclass(frozen=True)
    class SlackCredentials:
        oauth_client_id: str
        oauth_client_secret: SecretString
        bot_token: SecretString
        bot_refresh_token: SecretString
        user_token: SecretString
        user_refresh_token: SecretString

    slack_credentials = SlackCredentials(
        "client", SecretString("secret"), SecretString("old"), SecretString("bot-old"),
        SecretString("old"), SecretString("user-old"),
    )
    slack_fields = {name: name for name in (
        "oauth_client_id", "oauth_client_secret", "bot_token", "bot_refresh_token",
        "user_token", "user_refresh_token",
    )}
    scopes = slack_auth["oauth2"]["scopes"]
    user_scopes = slack_auth["oauth2"]["userScopes"]
    requests: list[str] = []

    def slack_response(request: httpx.Request) -> httpx.Response:
        prior = parse_qs(request.content.decode())["refresh_token"][0]
        requests.append(prior)
        if prior == "bot-old":
            return httpx.Response(200, json={
                "access_token": "bot-new", "refresh_token": "bot-rotated", "expires_in": 3600,
                "token_type": "bot", "scope": ",".join(scopes),
            })
        return httpx.Response(200, json={
            "access_token": "user-new", "refresh_token": "user-rotated", "expires_in": 3600,
            "token_type": "user", "scope": ",".join(user_scopes),
        })

    async with httpx.AsyncClient(transport=httpx.MockTransport(slack_response)) as client:
        slack_driver = ManifestCredentialRefreshDriver[SlackCredentials](
            "Slack", slack_auth, slack_fields, client
        )
        slack_result = await slack_driver.refresh(
            CredentialRefreshState(slack_credentials, None, datetime.now(UTC))
        )
        assert requests == ["bot-old", "user-old"]
        assert slack_result.credentials.user_refresh_token.reveal() == "user-rotated"
        with pytest.raises(ReauthorizationRequiredError):
            await slack_driver.refresh(CredentialRefreshState(
                SlackCredentials("client", SecretString(""), SecretString("old"), SecretString("bot-old"),
                                 SecretString("old"), SecretString("user-old")), None, datetime.now(UTC)
            ))
        assert requests == ["bot-old", "user-old"]


@pytest.mark.asyncio
async def test_manifest_validates_all_grants_and_go_durations_before_exchange() -> None:
    """A local mapping or lifetime error cannot consume a one-use refresh token."""
    auth: dict[str, Any] = deepcopy(AUTH)
    refresh = auth["methods"][0]["oauth2"]["refresh"]
    refresh["missingExpiry"] = {"lifetime": "1h2m3.5s4ms5us6ns"}
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, json={
            "access_token": "next", "refresh_token": "rotated", "token_type": "Bearer",
        })

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        driver = ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, client)
        now = datetime.now(UTC)
        state = CredentialRefreshState(
            Credentials(SecretString("old"), SecretString("prior")), None, now
        )
        result = await driver.refresh(state)
        assert result.credentials.refresh_token.reveal() == "rotated"
        assert result.expires_at > now + timedelta(seconds=3723)
        refresh["missingExpiry"] = {"lifetime": ".5h"}
        assert (await driver.refresh(state)).expires_at > now + timedelta(minutes=29)
        refresh["missingExpiry"] = {"lifetime": "+1h"}
        assert (await driver.refresh(state)).expires_at > now + timedelta(minutes=59)
        refresh["missingExpiry"] = {"lifetime": "0"}
        with pytest.raises(ValueError, match="duration"):
            await driver.refresh(state)
        refresh["missingExpiry"] = {"lifetime": "1fortnight"}
        with pytest.raises(ValueError, match="duration"):
            await driver.refresh(state)
        refresh["missingExpiry"] = {"lifetime": "2562048h"}
        with pytest.raises(ValueError, match="duration"):
            await driver.refresh(state)
        refresh["missingExpiry"] = {"lifetime": "-1h"}
        with pytest.raises(ValueError, match="duration"):
            await driver.refresh(state)
        assert len(requests) == 3
        refresh["missingExpiry"] = "keep"
        refresh["grants"] = [
            {"refreshTokenCredential": "refresh_token", "credentialMappings": [
                {"credential": "access_token", "source": "access_token"},
            ]},
            {"refreshTokenCredential": "refresh_token", "credentialMappings": [
                {"credential": "unknown", "source": "access_token"},
            ]},
        ]
        with pytest.raises(ValueError, match="mapped"):
            await driver.refresh(state)
        assert len(requests) == 3


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
        with pytest.raises(RetryError):
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
        assert not lifetime_driver.refresh_required(state)
        auth["methods"][0]["oauth2"]["refresh"]["storedMissingExpiry"] = "refresh"
        stored_expiry_driver = ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, client)
        assert stored_expiry_driver.refresh_required(state)


@pytest.mark.asyncio
async def test_stored_missing_expiry_refreshes_but_response_requires_expires_in() -> None:
    """An undated stored token refreshes while its replacement still requires expiry."""
    auth: dict[str, Any] = deepcopy(AUTH)
    refresh = auth["methods"][0]["oauth2"]["refresh"]
    refresh["storedMissingExpiry"] = "refresh"
    refresh["acceptsMissingTokenType"] = True
    response: dict[str, Any] = {"access_token": "next", "expires_in": 3600, "scope": "files.read"}
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, json=response)

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        driver = ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, client)
        state = CredentialRefreshState(
            Credentials(SecretString("old"), SecretString("prior")), None, datetime.now(UTC)
        )
        assert driver.refresh_required(state)
        result = await driver.refresh(state)
        assert result.credentials.access_token.reveal() == "next"
        response.pop("expires_in")
        with pytest.raises(RetryError, match="omitted required fields"):
            await driver.refresh(state)
        assert len(requests) == 2


@pytest.mark.parametrize("grant_kind", ["refresh", "client_credentials", "jwt_bearer"])
@pytest.mark.asyncio
async def test_stored_missing_expiry_is_independent_and_validated_for_every_grant(
    grant_kind: str,
) -> None:
    """A response lifetime does not refresh an undated credential for any grant."""
    if grant_kind == "refresh":
        auth: dict[str, Any] = deepcopy(AUTH)
        grant = auth["methods"][0]["oauth2"]["refresh"]
    elif grant_kind == "client_credentials":
        grant = {
            "tokenEndpoint": "https://oauth.example.test/token",
            "clientIDCredential": "client_id",
            "clientSecretCredential": "client_secret",
            "credentialMappings": [{"credential": "access_token", "source": "access_token"}],
        }
        auth = {"clientCredentials": grant}
    else:
        grant = {
            "tokenEndpoint": "https://oauth.example.test/token",
            "keyCredential": "client_secret",
            "credentialMappings": [{"credential": "access_token", "source": "access_token"}],
        }
        auth = {"serviceAccount": {"jwtBearer": grant}}

    state = CredentialRefreshState(
        Credentials(SecretString("old"), SecretString("prior")), None, datetime.now(UTC)
    )
    grant["missingExpiry"] = {"lifetime": "1h"}
    async with httpx.AsyncClient() as client:
        def driver() -> ManifestCredentialRefreshDriver[Credentials]:
            return ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, client)

        assert not driver().refresh_required(state)
        grant["storedMissingExpiry"] = "refresh"
        assert driver().refresh_required(state)
        grant["storedMissingExpiry"] = "keep"
        assert not driver().refresh_required(state)
        grant["storedMissingExpiry"] = "invalid"
        with pytest.raises(ValueError, match="storedMissingExpiry must be refresh or keep"):
            driver()


@pytest.mark.asyncio
async def test_manifest_missing_token_type_requires_declaration() -> None:
    """Atlassian-style responses can omit token_type when explicitly allowed."""
    auth: dict[str, Any] = deepcopy(AUTH)
    response: dict[str, Any] = {"access_token": "next", "refresh_token": "rotated", "expires_in": 3600}

    def respond(_: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json=response)

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        state = CredentialRefreshState(
            Credentials(SecretString("old"), SecretString("prior")), None, datetime.now(UTC)
        )
        strict = ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, client)
        with pytest.raises(RetryError):
            await strict.refresh(state)
        auth["methods"][0]["oauth2"]["refresh"]["acceptsMissingTokenType"] = True
        allowed = ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, client)
        result = await allowed.refresh(state)
        assert result.credentials.refresh_token.reveal() == "rotated"
        refresh = auth["methods"][0]["oauth2"]["refresh"]
        refresh["exactScopes"] = True
        refresh["grants"] = [{
            "refreshTokenCredential": "refresh_token",
            "credentialMappings": auth["methods"][0]["oauth2"]["credentialMappings"],
            "acceptsMissingTokenType": False,
            "exactScopes": False,
        }]
        overridden = ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, client)
        with pytest.raises(RetryError, match="required fields"):
            await overridden.refresh(state)
        refresh["grants"][0]["acceptsMissingTokenType"] = True
        response["scope"] = "files.read extra"
        await overridden.refresh(state)


@pytest.mark.asyncio
async def test_manifest_form_post_secret_and_optional_public_client() -> None:
    """Only an explicitly declared public client can refresh without a secret."""
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
        with pytest.raises(ReauthorizationRequiredError):
            await required_driver.refresh(empty_state)
        assert len(requests) == 1
        method["oauth2"]["refresh"]["allowsPublicClient"] = True
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
async def test_manifest_template_rejects_aliases_and_lowercases_valid_tenant() -> None:
    """An invalid tenant cannot reach the token transport; accepted domains use Go casing."""
    auth: dict[str, Any] = deepcopy(AUTH)
    auth["methods"][0]["oauth2"]["tokenEndpoint"] = {
        "template": "https://login.microsoftonline.com/{client_id}/oauth2/v2.0/token",
        "templateValueRules": {"client_id": {
            "trimSpace": True, "lowercase": True, "maxBytes": 253,
            "patterns": [
                r"^[0-9A-Fa-f]{8}(?:-[0-9A-Fa-f]{4}){3}-[0-9A-Fa-f]{12}$",
                r"^(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z]{2,63}$",
            ],
            "reject": ["common", "organizations", "consumers"],
        }},
    }
    auth["methods"][0]["oauth2"]["refresh"]["trimClientID"] = True
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, json={"access_token": "next", "expires_in": 3600, "token_type": "Bearer"})

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        driver = ManifestCredentialRefreshDriver[Credentials]("Microsoft", auth, FIELDS, client)

        async def refresh(tenant: str) -> None:
            state = CredentialRefreshState(
                Credentials(SecretString("old"), SecretString("prior"), client_id=tenant),
                None, datetime.now(UTC),
            )
            await driver.refresh(state)

        for tenant in ("common", " Organizations ", "CONSUMERS", "contoso", "a/b.com", "x" * 254):
            with pytest.raises(ReauthorizationRequiredError):
                await refresh(tenant)
        assert requests == []
        await refresh("  Contoso.OnMicrosoft.com  ")
        auth["methods"][0]["oauth2"]["refresh"]["trimClientID"] = False
        driver = ManifestCredentialRefreshDriver[Credentials]("Microsoft", auth, FIELDS, client)
        await refresh("  Contoso.OnMicrosoft.com  ")
    assert str(requests[0].url) == (
        "https://login.microsoftonline.com/contoso.onmicrosoft.com/oauth2/v2.0/token"
    )
    assert base64.b64decode(requests[0].headers["authorization"].split()[1]) == (
        b"Contoso.OnMicrosoft.com:client-secret"
    )
    assert base64.b64decode(requests[1].headers["authorization"].split()[1]) == (
        b"  Contoso.OnMicrosoft.com  :client-secret"
    )


@pytest.mark.asyncio
async def test_template_value_rules_match_complete_ascii_values_and_validate_at_construction() -> None:
    """Template alternatives and byte limits follow the cross-language grant contract."""
    auth: dict[str, Any] = deepcopy(AUTH)
    oauth = auth["methods"][0]["oauth2"]
    oauth["tokenEndpoint"] = {
        "template": "https://example.test/{client_id}",
        "templateValueRules": {"client_id": {
            "patterns": ["a|ab"], "maxBytes": 253, "lowercase": False,
        }},
    }
    rule = oauth["tokenEndpoint"]["templateValueRules"]["client_id"]
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, json={"access_token": "next", "expires_in": 3600, "token_type": "Bearer"})

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        def driver() -> ManifestCredentialRefreshDriver[Credentials]:
            return ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, client)

        async def refresh(value: str) -> None:
            state = CredentialRefreshState(
                Credentials(SecretString("old"), SecretString("prior"), client_id=value),
                None, datetime.now(UTC),
            )
            await driver().refresh(state)

        await refresh("ab")
        assert str(requests[-1].url) == "https://example.test/ab"
        rule["patterns"] = ["[A-Za-z]+"]
        await refresh("A" * 253)
        with pytest.raises(ReauthorizationRequiredError):
            await refresh("A" * 254)
        rule["maxBytes"] = 0
        with pytest.raises(ReauthorizationRequiredError):
            await refresh("A")
        rule["maxBytes"] = 253
        await refresh("AbC")
        assert str(requests[-1].url) == "https://example.test/AbC"
        rule["trimSpace"] = True
        await refresh("  AbC  ")
        assert base64.b64decode(requests[-1].headers["authorization"].split()[1]).startswith(
            b"  AbC  :"
        )
        for value in ("K", "ſ"):
            with pytest.raises(ReauthorizationRequiredError):
                await refresh(value)
        rule["patterns"] = [r"\d+"]
        with pytest.raises(ReauthorizationRequiredError):
            await refresh("١")
        for invalid_pattern in (r"(?=a)a", r"(a)\1", r"a)|(b", r"\p{L}", r"\Qa\E", r"[[:alpha:]]", r"(?i)a"):
            rule["patterns"] = [invalid_pattern]
            with pytest.raises(ValueError, match="pattern is invalid"):
                driver()
        rule["patterns"] = [r"\(?x"]
        driver()
        rule["patterns"] = [r"[A-Za-z]+"]
        for invalid_rule in ({"patterns": ["(?i)abc"]}, {"reject": [42]}, {"unknown": True}):
            rule.update(invalid_rule)
            with pytest.raises(ValueError):
                driver()
            for key in invalid_rule:
                del rule[key]


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
            "scopeSeparators": "whitespaceAndComma",
            "credentialMappings": [{"credential": "access_token", "source": "access_token"}],
        },
    }
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, json={
            "access_token": "next", "expires_in": 3600,
            "token_type": "Bearer", "scope": "files.read,offline_access",
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
        with pytest.raises(ReauthorizationRequiredError if is_terminal else RetryError):
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
        auth["methods"][0]["oauth2"]["refresh"]["exactScopes"] = True
        def extra_scope_respond(request: httpx.Request) -> httpx.Response:
            response = respond(request)
            payload = json.loads(response.content)
            payload["scope"] = "chat.write extra"
            return httpx.Response(200, json=payload)
        async with httpx.AsyncClient(transport=httpx.MockTransport(extra_scope_respond)) as extra_client:
            with pytest.raises(ReauthorizationRequiredError):
                await ManifestCredentialRefreshDriver[MultiGrantCredentials](
                    "Example", auth, fields, extra_client
                ).refresh(CredentialRefreshState(credentials, now, now))
        auth["methods"][0]["oauth2"]["refresh"]["exactScopes"] = False
        auth["methods"][0]["oauth2"]["refresh"]["missingExpiry"] = {"lifetime": "2h"}
        auth["methods"][0]["oauth2"]["refresh"]["storedMissingExpiry"] = "refresh"
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
                "subjectValidation": "goMailAddress",
                "trimSpaceSources": ["delegated_user"],
                "audience": {"fromKey": "token_uri"},
                "tokenEndpoint": {"fromKey": "token_uri"},
                "scopes": ["mail.read", "offline_access"],
                "acceptedScopeSets": [["mail.read"], ["mail.manage"]],
                "scopeSeparators": "whitespaceAndComma",
                "lifetime": "1h",
                "credentialMappings": [{"credential": "access_token", "source": "access_token"}],
                "credentialCopies": [{"credential": "primary_email", "fromCredential": "delegated_user"}],
            }
        },
    }
    requests: list[httpx.Request] = []
    returned_scope = "mail.read offline_access"

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(
            200,
            json={
                "access_token": "delegated-access",
                "expires_in": 3600,
                "token_type": "Bearer",
                "scope": returned_scope,
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
                    "unrelated_metadata": {"revision": 2},
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

    returned_scope = "mail.manage,extra"
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        driver = ManifestCredentialRefreshDriver[DelegatedCredentials]("Example", auth, fields, client)
        await driver.refresh(CredentialRefreshState(credentials, None, datetime.now(UTC)))
        await driver.refresh(CredentialRefreshState(
            replace(credentials, delegated_user="\u00a0user@example.test\u00a0"), None, datetime.now(UTC),
        ))
        spaced_assertion = parse_qs(requests[-1].content.decode())["assertion"][0]
        spaced_claims = json.loads(base64.urlsafe_b64decode(spaced_assertion.split(".")[1] + "=="))
        assert spaced_claims["sub"] == "user@example.test"
        returned_scope = "mail.other"
        with pytest.raises(ReauthorizationRequiredError):
            await driver.refresh(CredentialRefreshState(credentials, None, datetime.now(UTC)))

    invalid_auth: dict[str, Any] = deepcopy(auth)
    invalid_auth["serviceAccount"]["jwtBearer"]["lifetime"] = "invalid"
    invalid_requests: list[httpx.Request] = []

    def reject_request(request: httpx.Request) -> httpx.Response:
        invalid_requests.append(request)
        raise httpx.ConnectError("temporary network failure")

    async with httpx.AsyncClient(transport=httpx.MockTransport(reject_request)) as client:
        invalid = ManifestCredentialRefreshDriver[DelegatedCredentials](
            "Example", invalid_auth, fields, client
        )
        with pytest.raises(ValueError, match="duration"):
            await invalid.refresh(CredentialRefreshState(credentials, None, datetime.now(UTC)))
        invalid_auth["serviceAccount"]["jwtBearer"]["lifetime"] = "500us"
        with pytest.raises(ValueError, match="at least one millisecond"):
            await invalid.refresh(CredentialRefreshState(credentials, None, datetime.now(UTC)))
        assert not invalid_requests
        retryable = ManifestCredentialRefreshDriver[DelegatedCredentials](
            "Example", auth, fields, client
        )
        with pytest.raises(RetryError, match="unavailable"):
            await retryable.refresh(CredentialRefreshState(credentials, None, datetime.now(UTC)))


@pytest.mark.asyncio
async def test_manifest_alternative_scope_sets() -> None:
    """A returned scope can satisfy either declared set, including a broader one."""
    auth: dict[str, Any] = deepcopy(AUTH)
    refresh = auth["methods"][0]["oauth2"]["refresh"]
    refresh["acceptedScopeSets"] = [["files.read", "offline_access"], ["files.manage"]]
    returned_scope = ""

    def respond(_: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json={
            "access_token": "next", "expires_in": 3600, "token_type": "Bearer", "scope": returned_scope,
        })

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        driver = ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, client)
        state = CredentialRefreshState(
            Credentials(SecretString("old"), SecretString("prior")), None, datetime.now(UTC)
        )
        for scope in ("", "files.read", "files.manage", "files.manage extra"):
            returned_scope = scope
            await driver.refresh(state)
        returned_scope = "files.other"
        with pytest.raises(ReauthorizationRequiredError):
            await driver.refresh(state)
        refresh["exactScopes"] = True
        returned_scope = "files.manage extra"
        with pytest.raises(ReauthorizationRequiredError):
            await driver.refresh(state)


@pytest.mark.asyncio
async def test_manifest_delegated_subject_matches_go_mail_parse_address() -> None:
    """Go-derived address cases are rejected before token I/O when invalid."""
    private_key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    pem = private_key.private_bytes(
        serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()
    ).decode()
    key = SecretString(json.dumps({
        "private_key": pem, "client_email": "service@example.test",
        "token_uri": "https://oauth.example.test/token",
    }))
    auth: dict[str, Any] = {"type": "serviceAccount", "serviceAccount": {"jwtBearer": {
        "keyCredential": "service_account_key", "privateKey": {"fromKey": "private_key"},
        "issuer": {"fromKey": "client_email"}, "subject": {"fromCredential": "delegated_user"},
        "subjectValidation": "goMailAddress", "audience": {"fromKey": "token_uri"},
        "tokenEndpoint": {"fromKey": "token_uri"}, "scopes": ["mail.read"], "lifetime": "1h",
        "credentialMappings": [{"credential": "access_token", "source": "access_token"}],
    }}}
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, json={"access_token": "next", "expires_in": 3600, "token_type": "Bearer"})

    cases = [json.loads(line) for line in (
        Path(__file__).parent / "fixtures" / "go-mail-addresses.jsonl"
    ).read_text().splitlines()]
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        driver = ManifestCredentialRefreshDriver[DelegatedCredentials]("Google", auth, {
            "access_token": "access_token", "service_account_key": "service_account_key",
            "delegated_user": "delegated_user",
        }, client)
        for case in cases:
            before = len(requests)
            state = CredentialRefreshState(
                DelegatedCredentials(SecretString(""), key, case["input"]), None, datetime.now(UTC)
            )
            if case["accepted"]:
                await driver.refresh(state)
                assert len(requests) == before + 1, case["input"]
            else:
                with pytest.raises(ReauthorizationRequiredError):
                    await driver.refresh(state)
                assert len(requests) == before, case["input"]


@pytest.mark.asyncio
async def test_client_credentials_rejects_missing_positive_expiry_without_reauthorization() -> None:
    """Help Scout's strict expires_in failure retries a malformed success response."""
    auth: dict[str, Any] = {"fields": [{"name": "access_token", "type": "secretString"}],
        "clientCredentials": {
        "tokenEndpoint": "https://oauth.example.test/token", "clientIDCredential": "client_id",
        "clientSecretCredential": "client_secret", "missingExpiry": "reject",
        "credentialMappings": [{"credential": "access_token", "source": "access_token"}],
        "terminalErrorCodes": ["invalid_client"],
    }}
    response: dict[str, Any] = {"access_token": "next", "token_type": "Bearer"}
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, json=response)

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        driver = ManifestCredentialRefreshDriver[Credentials]("Help Scout", auth, FIELDS, client)
        state = CredentialRefreshState(
            Credentials(SecretString("old"), SecretString("prior")), None, datetime.now(UTC)
        )
        for expiry in (None, 0, -1):
            if expiry is None:
                response.pop("expires_in", None)
            else:
                response["expires_in"] = expiry
            with pytest.raises(RetryError, match="omitted required fields"):
                await driver.refresh(state)
        response["expires_in"] = 3600
        assert (await driver.refresh(state)).credentials.access_token.reveal() == "next"
        assert len(requests) == 4


@pytest.mark.parametrize("declaration", [
    {"missingExpiry": "unknown"}, {"acceptedScopeSets": []}, {"responseScopes": "unknown"},
    {"acceptedScopeSets": [["read"], []]}, {"scopeSeparators": "comma"},
    {"missingExpiry": None}, {"acceptedScopeSets": None}, {"scopeSeparators": None},
    {"missingExpiry": {"lifetime": "0s"}}, {"missingExpiry": {"lifetime": "nonsense"}},
])
@pytest.mark.asyncio
async def test_manifest_rejects_unknown_refresh_declarations(declaration: dict[str, Any]) -> None:
    """SDK callers cannot silently weaken schema-validated response rules."""
    auth: dict[str, Any] = deepcopy(AUTH)
    auth["methods"][0]["oauth2"]["refresh"].update(declaration)
    async with httpx.AsyncClient() as client:
        with pytest.raises(ValueError):
            ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, client)


@pytest.mark.asyncio
async def test_manifest_rejects_ignored_client_sets_and_invalid_jwt_declarations() -> None:
    """Declared validation must have a subject and supported response rules."""
    client_auth = {"clientCredentials": {"acceptedScopeSets": [["read"]],
        "tokenEndpoint": "https://oauth.example.test/token",
        "clientIDCredential": "client_id", "clientSecretCredential": "client_secret",
        "credentialMappings": [{"credential": "access_token", "source": "access_token"}]}}
    jwt_auth: dict[str, Any] = {"serviceAccount": {"jwtBearer": {"lifetime": "1h",
        "keyCredential": "service_account_key", "tokenEndpoint": "https://oauth.example.test/token",
        "credentialMappings": [{"credential": "access_token", "source": "access_token"}]}}}
    assertion = jwt_auth["serviceAccount"]["jwtBearer"]
    async with httpx.AsyncClient() as client:
        with pytest.raises(ValueError):
            ManifestCredentialRefreshDriver[Credentials]("Example", client_auth, FIELDS, client)
        for declaration in ({"subjectValidation": "other"}, {"subjectValidation": None},
                            {"subjectValidation": "goMailAddress"}, {"scopeSeparators": None},
                            {"scopeSeparators": "other"}, {"acceptedScopeSets": None}):
            assertion.update(declaration)
            with pytest.raises(ValueError):
                ManifestCredentialRefreshDriver[Credentials]("Example", jwt_auth, FIELDS, client)
            for key in declaration:
                del assertion[key]


@pytest.mark.asyncio
async def test_go_agreement_scope_expiry_and_client_secret_decisions() -> None:
    """Replay the three LinkedIn Go agreement failure classes through one grant."""
    auth: dict[str, Any] = deepcopy(AUTH)
    auth["methods"][0]["oauth2"]["refresh"].update({
        "requiredScopes": ["files.read"], "exactScopes": True, "missingExpiry": "reject",
    })
    response: dict[str, Any] = {
        "access_token": "next", "token_type": "Bearer", "expires_in": 3600, "scope": "files.read",
    }
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, json=response)

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        driver = ManifestCredentialRefreshDriver[Credentials]("LinkedIn", auth, FIELDS, client)
        state = CredentialRefreshState(
            Credentials(SecretString("old"), SecretString("prior")), None, datetime.now(UTC)
        )
        response["scope"] = "files.read extra"
        with pytest.raises(ReauthorizationRequiredError):
            await driver.refresh(state)
        response["scope"] = "files.read"
        response.pop("expires_in")
        with pytest.raises(RetryError, match="omitted required fields"):
            await driver.refresh(state)
        assert len(requests) == 2
        no_secret = CredentialRefreshState(
            Credentials(SecretString("old"), SecretString("prior"), client_secret=SecretString("")),
            None, datetime.now(UTC),
        )
        with pytest.raises(ReauthorizationRequiredError):
            await driver.refresh(no_secret)
        assert len(requests) == 2

@pytest.mark.asyncio
async def test_go_scope_array_graph_prefix_url_origin_and_expiry_cap() -> None:
    """Manifest response rules retain only a valid provider origin and cap Zoho-style expiry."""
    auth: dict[str, Any] = deepcopy(AUTH)
    refresh = auth["methods"][0]["oauth2"]["refresh"]
    refresh.update(
        {
            "requiredScopes": ["files.read"],
            "responseScopeField": "scopesArray",
            "scopeResourcePrefix": "https://graph.microsoft.com/",
            "caseInsensitiveScopes": True,
            "scopeResourcePrefixCaseInsensitive": True,
            "expiryCap": "1h",
            "responseURLValidation": {
                "credential": "instance_url",
                "allowedHosts": ["example.test"],
                "requireResponse": True,
                "refreshWhenInvalid": True,
            },
        }
    )
    body: dict[str, Any] = {
        "access_token": "next",
        "token_type": "Bearer",
        "expires_in": 7200,
        "scopes": ["HTTPS://GRAPH.MICROSOFT.COM/FILES.READ"],
        "instance_url": "https://example.test/",
    }

    def respond(_: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json=body)

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        driver = ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, client)
        assert driver.refresh_required(CredentialRefreshState(
            Credentials(SecretString("old"), SecretString("prior")),
            datetime.now(UTC) + timedelta(hours=1), datetime.now(UTC),
        ))
        state = CredentialRefreshState(
            Credentials(SecretString("old"), SecretString("prior")), None, datetime.now(UTC)
        )
        result = await driver.refresh(state)
        assert result.credentials.instance_url == "https://example.test"
        assert result.expires_at < datetime.now(UTC) + timedelta(seconds=3601)
        body["scopes"] = ["other"]
        with pytest.raises(ReauthorizationRequiredError):
            await driver.refresh(state)
        body["scopes"] = ["files.read,other"]
        with pytest.raises(ReauthorizationRequiredError):
            await driver.refresh(state)
        body["scopes"] = []
        with pytest.raises(ReauthorizationRequiredError):
            await driver.refresh(state)
        body["scope"] = "other"
        del body["scopes"]
        await driver.refresh(state)
        body["scopes"] = None
        await driver.refresh(state)
        body.update({"scopes": ["files.read"], "instance_url": "https://foreign.test"})
        with pytest.raises(RetryError):
            await driver.refresh(state)
        refresh["scopeResourcePrefixCaseInsensitive"] = False
        body.update({"scopes": ["HTTPS://GRAPH.MICROSOFT.COM/FILES.READ"], "instance_url": "https://example.test"})
        with pytest.raises(ReauthorizationRequiredError):
            await driver.refresh(state)
        refresh["scopeResourcePrefixCaseInsensitive"] = True
        body["instance_url"] = None
        prior_state = CredentialRefreshState(
            Credentials(SecretString("old"), SecretString("prior"), instance_url="https://example.test/"),
            None, datetime.now(UTC),
        )
        with pytest.raises(RetryError):
            await driver.refresh(prior_state)
        del body["instance_url"]
        with pytest.raises(RetryError):
            await driver.refresh(prior_state)


@pytest.mark.asyncio
async def test_zoho_response_url_host_comparison_is_case_sensitive() -> None:
    """A case-changed Zoho host is refused as Go ValidateAPIDomain requires."""
    auth: dict[str, Any] = deepcopy(AUTH)
    refresh = auth["methods"][0]["oauth2"]["refresh"]
    refresh["responseURLValidation"] = {
        "credential": "instance_url",
        "allowedHosts": [
            "www.zohoapis.com", "sandbox.zohoapis.com", "developer.zohoapis.com",
        ],
        "forbidPort": True,
    }
    body = {
        "access_token": "next", "token_type": "Bearer", "expires_in": 3600,
        "instance_url": "https://WWW.zohoapis.com",
    }

    async with httpx.AsyncClient(transport=httpx.MockTransport(
        lambda _: httpx.Response(200, json=body)
    )) as client:
        driver = ManifestCredentialRefreshDriver[Credentials]("Zoho", auth, FIELDS, client)
        state = CredentialRefreshState(
            Credentials(SecretString("old"), SecretString("prior"), instance_url="https://www.zohoapis.com"),
            None, datetime.now(UTC),
        )
        with pytest.raises(RetryError):
            await driver.refresh(state)
        body["instance_url"] = "https://www.zohoapis.com"
        assert (await driver.refresh(state)).credentials.instance_url == "https://www.zohoapis.com"
        del body["instance_url"]
        assert (await driver.refresh(state)).credentials.instance_url == "https://www.zohoapis.com"


@pytest.mark.asyncio
async def test_response_url_rules_and_blank_refresh_fields() -> None:
    """URL origins and blank refresh fields follow declared Go credential rules."""
    auth: dict[str, Any] = deepcopy(AUTH)
    refresh = auth["methods"][0]["oauth2"]["refresh"]
    refresh["refreshWhenCredentialBlank"] = "instance_url"
    refresh["responseURLValidation"] = {
        "credential": "instance_url", "allowedHosts": ["example.test"],
        "allowedHostSuffix": "example.test", "allowLoopback": True,
        "forbidPort": True, "defaultURL": "https://example.test", "refreshWhenInvalid": True,
    }
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, json={
            "access_token": "next", "token_type": "Bearer", "expires_in": 3600,
            "scope": "files.read",
        })

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        driver = ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, client)
        blank = CredentialRefreshState(
            Credentials(SecretString("old"), SecretString("prior"), instance_url=""),
            datetime.now(UTC) + timedelta(hours=1), datetime.now(UTC),
        )
        assert driver.refresh_required(blank)
        assert (await driver.refresh(blank)).credentials.instance_url == "https://example.test"
        for origin in ("http://[::1]", "http://127.0.0.2"):
            state = replace(blank, credentials=replace(blank.credentials, instance_url=origin))
            assert (await driver.refresh(state)).credentials.instance_url == origin
        for origin in (
            "https://example.test:8443", "https://example.test:443", "https://badexample.test",
            "http://127.1", "http://2130706433",
        ):
            state = replace(blank, credentials=replace(blank.credentials, instance_url=origin))
            with pytest.raises(RetryError):
                await driver.refresh(state)
        assert len(requests) == 8
        refresh["responseURLValidation"] = {"credential": "instance_url", "allowAnyHTTPS": True}
        any_https = ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, client)
        state = replace(blank, credentials=replace(blank.credentials, instance_url="https://other.test"))
        await any_https.refresh(state)
        for bad_rule in ({"credential": "instance_url", "unexpected": True}, {"allowAnyHTTPS": True}):
            refresh["responseURLValidation"] = bad_rule
            with pytest.raises(ValueError):
                ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, client)
        refresh["responseURLValidation"] = {}
        refresh["refreshWhenCredentialBlank"] = "missing"
        with pytest.raises(ValueError):
            ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, client)


@pytest.mark.asyncio
async def test_bad_token_endpoint_setting_retries() -> None:
    """Invalid configured endpoints remain retryable as in Go TokenEndpoint."""
    auth: dict[str, Any] = deepcopy(AUTH)
    auth["methods"][0]["oauth2"]["tokenEndpoint"] = {"fromSetting": "tokenEndpoint"}
    transport = httpx.MockTransport(lambda _: pytest.fail("unexpected request"))
    async with httpx.AsyncClient(transport=transport) as client:
        driver = ManifestCredentialRefreshDriver[Credentials](
            "Example", auth, FIELDS, client, settings={"tokenEndpoint": "not-a-url"},
        )
        with pytest.raises(RetryError):
            await driver.refresh(CredentialRefreshState(
                Credentials(SecretString("old"), SecretString("prior")), None, datetime.now(UTC),
            ))


@pytest.mark.parametrize(("payload_segment", "expected"), [
    ("eyJleHAiOjEyfQ", 12), ("eyJleHAiOjEyfQ==", 12),
    ("eyJleHAiOjEyM30=", 123), ("eyJleHAiOjEyMzR9====", 1234),
    ("eyJleHAiOjEyMzR9\r\n=", 1234), ("eyJleHAiOjEyMzR9A", None),
    ("eyJleHAiOjEyMzR9=\n", None),
])
def test_go_derived_jwt_expiry_decoding(payload_segment: str, expected: int | None) -> None:
    """Go RawURLEncoding and json.Unmarshal acceptance cases schedule the same expiry."""
    assert _read_jwt_expiry_seconds(f"e30.{payload_segment}.signature") == expected


def test_deep_jwt_claims_fall_back_without_recursive_json_parsing() -> None:
    """Large nesting remains an absent exp rather than a RecursionError."""
    claims = b'{"exp":1234,"nested":' + b"[" * 600 + b"0" + b"]" * 600 + b"}"
    payload = base64.urlsafe_b64encode(claims).decode().rstrip("=")
    assert _read_jwt_expiry_seconds(f"e30.{payload}.signature") is None


@pytest.mark.asyncio
async def test_jwt_expiry_and_undated_refresh_token_policy() -> None:
    """A monday-style JWT exp schedules refresh; absent refresh material keeps an undated token."""
    auth: dict[str, Any] = deepcopy(AUTH)
    refresh = auth["methods"][0]["oauth2"]["refresh"]
    refresh.update(
        {
            "missingExpiry": "keep",
            "storedMissingExpiry": "refreshIfRefreshTokenPresent",
            "accessTokenJWTExpiryFallback": "1h",
        }
    )
    exp = int(datetime.now(UTC).timestamp()) + 7200
    jwt = (
        "e30."
        + base64.urlsafe_b64encode(json.dumps({"exp": exp}).encode()).decode().rstrip("=")
        + ".signature"
    )
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(
            lambda _: httpx.Response(
                200,
                json={
                    "access_token": jwt,
                    "token_type": "Bearer",
                    "scope": "files.read",
                },
            )
        )
    ) as client:
        driver = ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, client)
        now = datetime.now(UTC)
        assert not driver.refresh_required(
            CredentialRefreshState(Credentials(SecretString(jwt), SecretString("prior")), None, now)
        )
        assert driver.refresh_required(
            CredentialRefreshState(Credentials(SecretString("bad"), SecretString("prior")), None, now)
        )
        assert not driver.refresh_required(
            CredentialRefreshState(Credentials(SecretString("bad"), SecretString("")), None, now)
        )
        result = await driver.refresh(
            CredentialRefreshState(Credentials(SecretString("old"), SecretString("prior")), None, now)
        )
        assert result.expires_at > now + timedelta(seconds=7100)
    past = "e30." + base64.urlsafe_b64encode(b'{"exp":1000}').decode().rstrip("=") + ".signature"
    async with httpx.AsyncClient(transport=httpx.MockTransport(lambda _: httpx.Response(200, json={
            "access_token": past, "token_type": "Bearer", "scope": "files.read",
        }))) as past_client:
        past_driver = ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, past_client)
        past_result = await past_driver.refresh(
            CredentialRefreshState(Credentials(SecretString("old"), SecretString("prior")), None, now)
        )
        assert now + timedelta(minutes=59) < past_result.expires_at < now + timedelta(minutes=61)


@pytest.mark.asyncio
async def test_manifest_token_endpoint_policy_refuses_forbidden_http_without_connecting() -> None:
    """A configurable token endpoint refuses forbidden addresses before TCP connect."""
    auth: dict[str, Any] = deepcopy(AUTH)
    oauth = auth["methods"][0]["oauth2"]
    oauth["tokenEndpoint"] = {"fromSetting": "tokenEndpoint"}
    oauth["tokenEndpointNetworkPolicy"] = "privateNetworkHTTP"
    with patch.object(
        httpcore.AnyIOBackend, "connect_tcp", side_effect=AssertionError("forbidden connection")
    ) as connect:
        async with httpx.AsyncClient() as client:
            state = CredentialRefreshState(
                Credentials(SecretString("old"), SecretString("prior")), None, datetime.now(UTC)
            )
            for token_endpoint in (
                "http://169.254.169.254/token", "http://[fe80::1]/token",
                "http://[fd00:ec2::254]/token", "http://[::ffff:169.254.169.254]/token",
                "http://2852039166/token", "http://0251.0376.0251.0376/token",
                "http://169.254.1/token", "http://8.8.8.8/token",
            ):
                driver = ManifestCredentialRefreshDriver[Credentials](
                    "Example", auth, FIELDS, client, settings={"tokenEndpoint": token_endpoint},
                )
                with pytest.raises(AddressRefusal):
                    await driver.refresh(state)
                assert connect.call_count == 0, token_endpoint
            for token_endpoint in (
                "https://identity.example/token#fragment", "https://identity.example/a b",
                "https://identity.example/é", "https://identity.example/\tpath",
            ):
                driver = ManifestCredentialRefreshDriver[Credentials](
                    "Example", auth, FIELDS, client, settings={"tokenEndpoint": token_endpoint},
                )
                with pytest.raises(ReauthorizationRequiredError):
                    await driver.refresh(state)
                assert connect.call_count == 0, token_endpoint
