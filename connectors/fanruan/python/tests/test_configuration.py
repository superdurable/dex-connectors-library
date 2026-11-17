# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Audit Dex Web fields visible for the runnable Jiandaoyun Flow."""

from pathlib import Path

import yaml
from dex_connectors.sdk import SecretString

from dex_connectors.fanruan import Credentials

MANIFEST = Path(__file__).parents[2] / "connector.yaml"


def test_application_directory_configuration_guidance() -> None:
    """Authorization and configuration fields explain provider source, format, and blanks."""
    spec = yaml.safe_load(MANIFEST.read_text())["spec"]
    assert spec["auth"]["guide"]["startURL"] == "https://www.jiandaoyun.com/"
    assert {field["name"] for field in spec["auth"]["fields"]} == {"api_key"}
    key_field = spec["auth"]["fields"][0]
    assert key_field["type"] == "secretString" and key_field["required"]
    assert "https://www.jiandaoyun.com/" in key_field["description"]
    assert "密钥管理" in key_field["description"]
    assert "blank" in key_field["description"].lower()
    fields = spec["configuration"]["fields"]
    assert {field["name"] for field in fields} == {"timeout", "maxResponseBytes"}
    for field in fields:
        assert field["default"] is not None
        assert "displayed default" in field["description"]
    assert spec["operations"][0].get("configurationUI") is None
    assert "secret-api-key" not in repr(Credentials(SecretString("secret-api-key")))
