# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check Sage Intacct connection guidance and secret-safe settings."""

from pathlib import Path

import yaml
from dex_connectors.sdk import SecretString

from dex_connectors.sage_intacct import Credentials

MANIFEST = yaml.safe_load(Path(__file__).parents[2].joinpath("connector.yaml").read_text())


def test_authorization_and_configuration_guidance() -> None:
    """Every visible connection field explains its source and blank behavior."""
    spec = MANIFEST["spec"]
    fields = {field["name"]: field for field in spec["configuration"]["fields"]}
    assert set(fields) == {"baseUrl", "timeout", "maxResponseBytes"}
    assert all(field["description"] and "Blank" in field["description"] for field in fields.values())
    assert fields["baseUrl"]["default"] == "https://api.intacct.com"
    assert fields["timeout"]["default"] == "15s"
    assert fields["maxResponseBytes"]["default"] == 1048576
    auth = spec["auth"]
    assert auth["refreshable"] and auth["type"] == "oauth2"
    assert auth["guide"]["startURL"] == "https://app-registry.sage.com/"
    guide = " ".join(auth["guide"]["steps"])
    assert "Create API Keys" in guide and "OAuth Redirect URL" in guide
    assert "offline_access" in guide and "Accounts Receivable" in guide
    credential_fields = {field["name"]: field for field in auth["fields"]}
    assert set(credential_fields) == {
        "oauth_client_id", "oauth_client_secret", "access_token", "refresh_token"
    }
    assert all(
        "Blank" in field["description"] or "blank" in field["description"]
        for field in credential_fields.values()
    )
    assert all(
        credential_fields[name]["type"] == "secretString"
        for name in ("oauth_client_secret", "access_token", "refresh_token")
    )
    assert auth["oauth2"]["scopes"] == ["offline_access"]


def test_secret_values_remain_redacted_in_credentials_representation() -> None:
    """Client secret and renewable tokens cannot leak through repr."""
    credentials = Credentials(
        "public-id", SecretString("client-secret-marker"), SecretString("access-marker"),
        SecretString("refresh-marker"),
    )
    representation = repr(credentials)
    assert "public-id" in representation
    for secret in ("client-secret-marker", "access-marker", "refresh-marker"):
        assert secret not in representation
