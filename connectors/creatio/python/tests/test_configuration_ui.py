# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check Creatio connection guidance and secret-safe settings."""

from pathlib import Path

import yaml
from dex_connectors.sdk import SecretString

from dex_connectors.creatio import Credentials

MANIFEST = yaml.safe_load(Path(__file__).parents[2].joinpath("connector.yaml").read_text())


def test_authorization_and_configuration_guidance() -> None:
    """Every visible connection field explains where to find its value."""
    spec = MANIFEST["spec"]
    fields = {field["name"]: field for field in spec["configuration"]["fields"]}
    assert set(fields) == {"baseUrl", "tokenEndpoint", "timeout", "maxResponseBytes"}
    assert all(field["description"] and "Blank" in field["description"] for field in fields.values())
    assert fields["baseUrl"]["required"] and fields["tokenEndpoint"]["required"]
    assert "OAuth20IdentityServerUrl" in fields["tokenEndpoint"]["description"]
    assert "/connect/token" in fields["tokenEndpoint"]["description"]
    assert fields["timeout"]["default"] == "15s"
    assert fields["maxResponseBytes"]["default"] == 1048576
    auth = spec["auth"]
    assert auth["refreshable"] and auth["type"] == "apiKey"
    assert auth["guide"]["startURL"].startswith("https://academy.creatio.com/")
    guide = " ".join(auth["guide"]["steps"])
    assert "System Designer" in guide and "Contact" in guide
    credential_fields = {field["name"]: field for field in auth["fields"]}
    assert set(credential_fields) == {"client_id", "client_secret", "access_token"}
    assert credential_fields["client_secret"]["type"] == "secretString"
    assert credential_fields["access_token"]["type"] == "secretString"
    assert auth["clientCredentials"]["tokenEndpoint"] == {"fromSetting": "tokenEndpoint"}
    assert auth["clientCredentials"]["credentialMappings"] == [
        {"credential": "access_token", "source": "access_token"}
    ]


def test_secret_values_remain_redacted_in_credentials_representation() -> None:
    """UI and logs cannot expose client secrets or minted tokens through repr."""
    credentials = Credentials("public-id", SecretString("client-secret-marker"), SecretString("token-marker"))
    representation = repr(credentials)
    assert "public-id" in representation
    assert "client-secret-marker" not in representation
    assert "token-marker" not in representation
