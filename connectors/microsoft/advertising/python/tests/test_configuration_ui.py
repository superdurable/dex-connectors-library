# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check Microsoft Advertising setup guidance and secret-safe settings."""

from pathlib import Path

import yaml
from dex_connectors.sdk import SecretString

from dex_connectors.microsoft_advertising import Credentials

MANIFEST = yaml.safe_load(Path(__file__).parents[2].joinpath("connector.yaml").read_text())


def test_authorization_and_configuration_guidance() -> None:
    """Every visible field explains its source, units, and blank behavior."""
    spec = MANIFEST["spec"]
    fields = {field["name"]: field for field in spec["configuration"]["fields"]}
    assert set(fields) == {"baseUrl", "customerId", "timeout", "maxResponseBytes"}
    assert all("blank" in field["description"].lower() for field in fields.values())
    assert fields["customerId"]["required"]
    assert "https://ui.ads.microsoft.com" in fields["customerId"]["description"]
    assert fields["baseUrl"]["default"] == "https://campaign.api.bingads.microsoft.com"
    assert fields["timeout"]["default"] == "15s"
    assert fields["maxResponseBytes"]["default"] == 4194304
    auth = spec["auth"]
    assert auth["refreshable"] and auth["type"] == "oauth2"
    assert auth["guide"]["startURL"] == "https://entra.microsoft.com/"
    guide = " ".join(auth["guide"]["steps"])
    for guidance in (
        "App registrations",
        "redirect URI",
        "msads.manage",
        "offline_access",
        "Developer Token",
    ):
        assert guidance in guide
    credentials = {field["name"]: field for field in auth["fields"]}
    assert set(credentials) == {
        "oauth_client_id",
        "oauth_client_secret",
        "developer_token",
        "access_token",
        "refresh_token",
    }
    assert all("blank" in field["description"].lower() for field in credentials.values())
    for name in ("oauth_client_secret", "developer_token", "access_token", "refresh_token"):
        assert credentials[name]["type"] == "secretString"
    oauth = auth["oauth2"]
    assert oauth["scopes"] == ["offline_access", "https://ads.microsoft.com/msads.manage"]
    assert oauth["tokenEndpointAuthMethod"] == "client_secret_post"
    assert oauth["pkce"]


def test_secret_values_remain_redacted_in_credentials_representation() -> None:
    """Client secret, developer token, and OAuth tokens remain redacted."""
    credentials = Credentials(
        "public-id",
        SecretString("client-secret-marker"),
        SecretString("developer-token-marker"),
        SecretString("access-marker"),
        SecretString("refresh-marker"),
    )
    representation = repr(credentials)
    assert "public-id" in representation
    for secret in ("client-secret-marker", "developer-token-marker", "access-marker", "refresh-marker"):
        assert secret not in representation
