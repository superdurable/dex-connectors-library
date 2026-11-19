# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Audit Dex Web fields visible for the runnable EasyAPI shop Flow."""

from pathlib import Path

import yaml
from dex_connectors.sdk import SecretString

from dex_connectors.easyapi import Credentials

MANIFEST = Path(__file__).parents[2] / "connector.yaml"


def test_shop_connection_configuration_guidance() -> None:
    """Each visible authorization and connection field explains provider setup and blanks."""
    spec = yaml.safe_load(MANIFEST.read_text())["spec"]
    assert spec["auth"]["guide"]["startURL"] == "https://fapiao.easyapi.com/docs/auth/"
    assert {field["name"] for field in spec["auth"]["fields"]} == {"app_key", "app_secret"}
    for field in spec["auth"]["fields"]:
        assert field["required"] and field["type"] == "secretString"
        assert "blank" in field["description"].lower()
        assert "服务认证" in field["description"]
    fields = spec["configuration"]["fields"]
    assert {field["name"] for field in fields} == {"baseUrl", "timeout", "maxResponseBytes"}
    for field in fields:
        assert field["default"] is not None
        assert "Leave blank" in field["description"]
    assert spec["operations"][0].get("configurationUI") is None
    credentials = Credentials(SecretString("test-key"), SecretString("private-secret"))
    assert "test-key" not in repr(credentials)
    assert "private-secret" not in repr(credentials)
