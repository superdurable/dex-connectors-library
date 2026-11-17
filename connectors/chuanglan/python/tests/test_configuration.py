# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Audit the fields visible for the runnable balance Flow in Dex Web."""

from pathlib import Path

import yaml
from dex_connectors.sdk import SecretString

from dex_connectors.chuanglan import Credentials

MANIFEST = Path(__file__).parents[2] / "connector.yaml"


def test_balance_connection_configuration_guidance() -> None:
    """Authorization and configuration fields explain origin, units, and blank defaults."""
    manifest = yaml.safe_load(MANIFEST.read_text())
    spec = manifest["spec"]
    assert spec["auth"]["guide"]["startURL"] == "https://zz.253.com"
    assert "产品概览 > API 接口信息" in " ".join(spec["auth"]["guide"]["steps"])
    assert {field["name"] for field in spec["auth"]["fields"]} == {"account", "password"}
    for field in spec["auth"]["fields"]:
        assert field["type"] == "secretString" and field["required"]
        assert "https://zz.253.com" in field["description"]
        assert "Secret" in field["description"] and "blank" in field["description"]
    fields = spec["configuration"]["fields"]
    assert {field["name"] for field in fields} == {"baseUrl", "timeout", "maxResponseBytes"}
    for field in fields:
        assert field["default"] is not None
        assert "Leave blank" in field["description"]
    assert spec["operations"][0].get("configurationUI") is None
    credentials = Credentials(SecretString("N9000123"), SecretString("inert-password"))
    assert "inert-password" not in repr(credentials)
