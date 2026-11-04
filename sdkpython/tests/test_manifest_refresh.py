# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest refresh contracts against a fake provider token endpoint."""

import base64
import json
import pickle
from copy import deepcopy
from dataclasses import asdict, dataclass, field
from datetime import UTC, datetime, timedelta
from pathlib import Path
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
        with pytest.raises(ValueError, match="required fields"):
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
async def test_schema_decoded_jira_and_slack_auth_refresh_with_planned_fields() -> None:
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
        assert not lifetime_driver.refresh_required(state)
        auth["methods"][0]["oauth2"]["refresh"]["storedMissingExpiry"] = "refresh"
        stored_expiry_driver = ManifestCredentialRefreshDriver[Credentials]("Example", auth, FIELDS, client)
        assert stored_expiry_driver.refresh_required(state)


@pytest.mark.asyncio
async def test_atlassian_stored_missing_expiry_refreshes_but_response_requires_expires_in() -> None:
    """Go Atlassian refreshes an undated stored token and rejects an undated replacement."""
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
        driver = ManifestCredentialRefreshDriver[Credentials]("Atlassian", auth, FIELDS, client)
        state = CredentialRefreshState(
            Credentials(SecretString("old"), SecretString("prior")), None, datetime.now(UTC)
        )
        assert driver.refresh_required(state)
        result = await driver.refresh(state)
        assert result.credentials.access_token.reveal() == "next"
        response.pop("expires_in")
        with pytest.raises(ValueError, match="omitted required fields"):
            await driver.refresh(state)
        assert len(requests) == 2


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
        with pytest.raises(ValueError):
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
        with pytest.raises(ValueError, match="required fields"):
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
        with pytest.raises(OSError, match="unavailable"):
            await retryable.refresh(CredentialRefreshState(credentials, None, datetime.now(UTC)))


@pytest.mark.asyncio
async def test_manifest_alternative_scope_sets() -> None:
    """A returned scope can satisfy either declared set, including a broader one."""
    auth: dict[str, Any] = deepcopy(AUTH)
    refresh = auth["methods"][0]["oauth2"]["refresh"]
    refresh["acceptedScopeSets"] = [["files.read"], ["files.manage"]]
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
