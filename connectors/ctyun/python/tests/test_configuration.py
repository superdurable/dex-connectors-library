# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Audit authorization and connection fields visible in Dex Web."""

from pathlib import Path

import yaml
from dex_connectors.sdk import SecretString

from dex_connectors.ctyun import Credentials

MANIFEST = Path(__file__).parents[2] / "connector.yaml"


def test_directory_connection_configuration_guidance() -> None:
    """Every visible field has provider navigation, format, and blank guidance."""
    spec = yaml.safe_load(MANIFEST.read_text())["spec"]
    assert spec["auth"]["guide"]["startURL"].startswith("https://www.ctyun.cn/")
    assert len(spec["auth"]["guide"]["steps"]) >= 2
    assert {field["name"] for field in spec["auth"]["fields"]} == {
        "access_key", "security_key", "tenant_id"
    }
    for field in spec["auth"]["fields"]:
        assert field["type"] == "secretString" and field["required"]
        assert "https://www.ctyun.cn/" in field["description"]
        assert "secret" in field["description"].lower()
        assert "blank" in field["description"].lower()
    fields = spec["configuration"]["fields"]
    assert {field["name"] for field in fields} == {"baseUrl", "timeout", "maxResponseBytes"}
    for field in fields:
        assert field["default"] is not None
        assert "Leave blank" in field["description"]
    assert spec["operations"][0].get("configurationUI") is None
    credentials = Credentials(
        SecretString("AKIDEXAMPLE"),
        SecretString("SKEXAMPLE"),
        SecretString("tenant-example"),
    )
    assert "SKEXAMPLE" not in repr(credentials)
    assert "tenant-example" not in repr(credentials)
