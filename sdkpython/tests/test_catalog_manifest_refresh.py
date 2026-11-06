# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Every catalog token-grant declaration must construct a manifest refresh driver."""

import base64
import json
from dataclasses import dataclass
from datetime import UTC, datetime
from pathlib import Path
from typing import Any
from urllib.parse import parse_qs

import httpx
import pytest
import yaml
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import rsa

from dex_connectors.sdk import CredentialRefreshState, SecretString
from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver

REPOSITORY = Path(__file__).resolve().parents[2]


def _has_token_grant(auth: dict[str, Any]) -> bool:
    methods = auth.get("methods") or [auth]
    for method in methods:
        if method.get("clientCredentials") or (method.get("serviceAccount") or {}).get("jwtBearer"):
            return True
        oauth = method.get("oauth2") or {}
        refresh = oauth.get("refresh") or {}
        if refresh.get("grants") or any(
            mapping.get("source") == "refresh_token" for mapping in oauth.get("credentialMappings", [])
        ):
            return True
    return False


def _referenced_setting_names(value: Any) -> set[str]:
    names: set[str] = set()
    if isinstance(value, dict):
        if isinstance(value.get("fromSetting"), str):
            names.add(value["fromSetting"])
        for nested in value.values():
            names.update(_referenced_setting_names(nested))
    elif isinstance(value, list):
        for nested in value:
            names.update(_referenced_setting_names(nested))
    return names


def _token_grant_manifests() -> list[tuple[str, dict[str, Any], dict[str, Any]]]:
    catalog = yaml.safe_load((REPOSITORY / "catalog.yaml").read_text())
    manifests: list[tuple[str, dict[str, Any], dict[str, Any]]] = []
    for directory in catalog["directories"]:
        manifest = yaml.safe_load((REPOSITORY / directory / "connector.yaml").read_text())
        auth = manifest["spec"].get("auth") or {}
        if _has_token_grant(auth):
            manifests.append((directory, auth, manifest["spec"].get("configuration") or {}))
    return manifests


TOKEN_GRANT_MANIFESTS = _token_grant_manifests()
assert len(TOKEN_GRANT_MANIFESTS) >= 35
assert any(directory == "connectors/salesforce" for directory, _, _ in TOKEN_GRANT_MANIFESTS)


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "directory,auth,configuration", TOKEN_GRANT_MANIFESTS, ids=[item[0] for item in TOKEN_GRANT_MANIFESTS]
)
async def test_catalog_token_grant_constructs(
    directory: str, auth: dict[str, Any], configuration: dict[str, Any]
) -> None:
    """Validate every declared grant shape, including Salesforce's credential-backed JWT key."""
    fields = list(auth.get("fields") or [])
    for method in auth.get("methods") or []:
        fields.extend(method.get("fields") or [])
    credential_fields = {field["name"]: field["name"] for field in fields}
    setting_names = _referenced_setting_names(auth)
    settings = {
        field["name"]: field["default"]
        for field in configuration.get("fields") or []
        if field["name"] in setting_names and isinstance(field.get("default"), (str, int))
    }
    async with httpx.AsyncClient(transport=httpx.MockTransport(lambda _: httpx.Response(500))) as client:
        driver = ManifestCredentialRefreshDriver[dict[str, object]](
            directory, auth, credential_fields, client, settings=settings,
        )
    assert driver is not None


@dataclass(frozen=True, slots=True)
class SalesforceJWTCredentials:
    """The credential fields read by Salesforce's JWT bearer grant."""

    auth_method_id: str
    oauth_client_id: str
    jwt_username: str
    jwt_private_key: SecretString
    jwt_login_environment: str
    access_token: SecretString
    instance_url: str


@pytest.mark.asyncio
async def test_salesforce_credential_private_key_mints_without_key_credential() -> None:
    """Replay the manifest's credential-backed JWT grant through a signed token request."""
    auth = next(auth for directory, auth, _ in TOKEN_GRANT_MANIFESTS if directory == "connectors/salesforce")
    private_key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    private_key_pem = private_key.private_bytes(
        serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()
    ).decode()
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, json={
            "access_token": "minted", "instance_url": "https://example.my.salesforce.com",
            "expires_in": 3600, "token_type": "Bearer", "scope": "api",
        })

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        driver = ManifestCredentialRefreshDriver[SalesforceJWTCredentials](
            "Salesforce", auth,
            {name: name for name in (
                "oauth_client_id", "jwt_username", "jwt_private_key", "jwt_login_environment",
                "access_token", "instance_url",
            )}, client,
        )
        credentials = SalesforceJWTCredentials(
            "salesforce-jwt-bearer", "consumer-key", "integration@example.com",
            SecretString(private_key_pem), "sandbox", SecretString(""), "",
        )
        result = await driver.refresh(CredentialRefreshState(credentials, None, datetime.now(UTC)))
    assert len(requests) == 1
    assert str(requests[0].url) == "https://test.salesforce.com/services/oauth2/token"
    assertion = parse_qs(requests[0].content.decode())["assertion"][0]
    claims = json.loads(base64.urlsafe_b64decode(assertion.split(".")[1] + "=="))
    assert claims["iss"] == "consumer-key"
    assert claims["sub"] == "integration@example.com"
    assert claims["aud"] == "https://test.salesforce.com"
    assert result.credentials.access_token.reveal() == "minted"
    assert result.credentials.instance_url == "https://example.my.salesforce.com"
