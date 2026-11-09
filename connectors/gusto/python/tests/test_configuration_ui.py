# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check Gusto connection guidance before it is rendered in Dex Web."""

from pathlib import Path

import yaml
from dex_connectors.sdk import SecretString

from dex_connectors.gusto import Credentials

MANIFEST = yaml.safe_load(Path(__file__).parents[2].joinpath("connector.yaml").read_text())


def test_authorization_and_configuration_guidance() -> None:
    """Every visible field has specific setup guidance and documented defaults."""
    spec = MANIFEST["spec"]
    fields = spec["configuration"]["fields"]
    assert {field["name"] for field in fields} == {
        "redirectUri",
        "baseUrl",
        "timeout",
        "maxResponseBytes",
    }
    descriptions = {field["name"]: field["description"] for field in fields}
    assert all(description and "Blank" in description for description in descriptions.values())
    assert "duration" in descriptions["timeout"]
    assert "bytes" in descriptions["maxResponseBytes"]
    assert "https://dev.gusto.com/" in descriptions["redirectUri"]
    assert (
        next(field for field in fields if field["name"] == "baseUrl")["default"]
        == "https://api.gusto.com"
    )
    authorization = spec["auth"]
    assert authorization["guide"]["startURL"] == "https://dev.gusto.com/"
    assert "redirect URI" in " ".join(authorization["guide"]["steps"])
    credential_fields = {field["name"]: field for field in authorization["fields"]}
    assert "https://dev.gusto.com/ > Applications" in credential_fields["oauth_client_id"]["description"]
    assert all(
        credential_fields[name]["type"] == "secretString"
        for name in ("oauth_client_secret", "access_token", "refresh_token")
    )
    assert authorization["oauth2"]["scopes"] == ["employees:read"]


def test_secret_values_remain_redacted_in_credentials_representation() -> None:
    """Dex UI and logs cannot expose the OAuth secrets through repr."""
    credentials = Credentials(
        "public-id",
        SecretString("client-secret-marker"),
        SecretString("access-token-marker"),
        SecretString("refresh-token-marker"),
    )
    representation = repr(credentials)
    assert "public-id" in representation
    assert all(
        marker not in representation
        for marker in ("client-secret-marker", "access-token-marker", "refresh-token-marker")
    )
