# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Audit the visible Dex Web fields for the runnable user read Flow."""

from pathlib import Path

import yaml
from dex_connectors.sdk import SecretString

from dex_connectors.easemob import Credentials

MANIFEST = Path(__file__).parents[2] / "connector.yaml"


def test_user_connection_configuration_guidance() -> None:
    """Each field explains the provider source, format, and blank behavior."""
    spec = yaml.safe_load(MANIFEST.read_text())["spec"]
    assert spec["auth"]["guide"]["startURL"] == "https://console.easemob.com"
    assert {field["name"] for field in spec["auth"]["fields"]} == {
        "appKey", "clientId", "clientSecret"
    }
    for field in spec["auth"]["fields"]:
        assert field["required"]
        assert "https://console.easemob.com" in field["description"]
        assert "Blank" in field["description"]
    secret_field = next(
        field for field in spec["auth"]["fields"] if field["name"] == "clientSecret"
    )
    assert secret_field["type"] == "secretString"
    fields = spec["configuration"]["fields"]
    assert {field["name"] for field in fields} == {"baseUrl", "timeout", "maxResponseBytes"}
    for field in fields:
        assert "blank" in field["description"].lower()
        if field["name"] != "baseUrl":
            assert field["default"] is not None
    assert spec["operations"][0].get("configurationUI") is None
    credentials = Credentials("demo#chat", "client-id", SecretString("client-secret"))
    assert "client-secret" not in repr(credentials)
