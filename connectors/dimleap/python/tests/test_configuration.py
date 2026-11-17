# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Audit connection fields for the runnable model catalog Flow in Dex Web."""

from pathlib import Path

import yaml
from dex_connectors.sdk import SecretString

from dex_connectors.dimleap import Credentials

MANIFEST = Path(__file__).parents[2] / "connector.yaml"


def test_catalog_connection_configuration_guidance() -> None:
    """Authorization and configuration fields explain source, units, and blanks."""
    spec = yaml.safe_load(MANIFEST.read_text())["spec"]
    assert spec["auth"]["guide"]["startURL"] == "https://www.dimleap.cn/docs"
    assert {field["name"] for field in spec["auth"]["fields"]} == {"apiKey"}
    key_field = spec["auth"]["fields"][0]
    assert key_field["type"] == "secretString" and key_field["required"]
    assert "https://www.dimleap.cn" in key_field["description"]
    assert "Secret" in key_field["description"]
    assert "blank" in key_field["description"]
    fields = spec["configuration"]["fields"]
    assert {field["name"] for field in fields} == {"timeout", "maxResponseBytes"}
    for field in fields:
        assert field["default"] is not None
        assert "Blank" in field["description"]
    assert spec["operations"][0].get("configurationUI") is None
    assert "sk-test-secret" not in repr(Credentials(SecretString("sk-test-secret")))
