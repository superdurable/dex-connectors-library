# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Audit Dex Web fields visible for the runnable Enlvon search Flow."""

from pathlib import Path

import yaml
from dex_connectors.sdk import SecretString

from dex_connectors.enlvon import Credentials

MANIFEST = Path(__file__).parents[2] / "connector.yaml"


def test_policy_connection_configuration_guidance() -> None:
    """Authorization and configuration fields explain provider source, format, and blanks."""
    spec = yaml.safe_load(MANIFEST.read_text())["spec"]
    assert spec["auth"]["guide"]["startURL"] == "https://enlvon.com/docs"
    assert {field["name"] for field in spec["auth"]["fields"]} == {"api_key"}
    key_field = spec["auth"]["fields"][0]
    assert key_field["type"] == "secretString" and key_field["required"]
    assert "https://enlvon.com/apply" in key_field["description"]
    assert "enl_" in key_field["description"]
    assert "Blank" in key_field["description"]
    fields = spec["configuration"]["fields"]
    assert {field["name"] for field in fields} == {"baseUrl", "timeout", "maxResponseBytes"}
    for field in fields:
        assert field["default"] is not None
        assert "Leave blank" in field["description"]
    assert spec["operations"][0].get("configurationUI") is None
    assert "enl_test-secret" not in repr(Credentials(SecretString("enl_test-secret")))
