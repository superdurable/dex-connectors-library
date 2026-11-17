# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Audit the fields visible for the runnable balance Flow in Dex Web."""

from pathlib import Path

import yaml
from dex_connectors.sdk import SecretString

from dex_connectors.cloopen import Credentials

MANIFEST = Path(__file__).parents[2] / "connector.yaml"


def test_balance_connection_configuration_guidance() -> None:
    """Authorization and configuration fields explain origin, units, and blank defaults."""
    manifest = yaml.safe_load(MANIFEST.read_text())
    spec = manifest["spec"]
    assert spec["auth"]["guide"]["startURL"] == "https://console.yuntongxun.com"
    assert "account information" in " ".join(spec["auth"]["guide"]["steps"])
    assert {field["name"] for field in spec["auth"]["fields"]} == {"account_sid", "auth_token"}
    for field in spec["auth"]["fields"]:
        assert field["type"] == "secretString" and field["required"]
        assert "https://console.yuntongxun.com" in field["description"]
        assert "secret" in field["description"].lower()
        assert "blank" in field["description"].lower()
    fields = spec["configuration"]["fields"]
    assert {field["name"] for field in fields} == {"baseUrl", "timeout", "maxResponseBytes"}
    for field in fields:
        assert field["default"] is not None
        assert "Leave blank" in field["description"]
    assert spec["operations"][0].get("configurationUI") is None
    credentials = Credentials(
        SecretString("abcdefghijklmnopqrstuvwxyz012345"), SecretString("012345abcdefghijklmnopqrstuvwxyz")
    )
    assert "012345abcdefghijklmnopqrstuvwxyz" not in repr(credentials)
