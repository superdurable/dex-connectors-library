# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check the guidance shown for every Segment connection field."""

from pathlib import Path

import yaml


def test_connection_fields_have_actionable_guidance() -> None:
    """Every visible field explains its source, format, blank behavior, and region."""
    manifest = yaml.safe_load(Path(__file__).parents[2].joinpath("connector.yaml").read_text())
    configuration_fields = manifest["spec"]["configuration"]["fields"]
    credential_fields = manifest["spec"]["auth"]["fields"]
    assert {field["name"] for field in configuration_fields} == {
        "baseUrl",
        "timeout",
        "maxResponseBytes",
    }
    assert {field["name"] for field in credential_fields} == {"api_key"}
    assert all("blank" in field["description"].lower() for field in configuration_fields + credential_fields)
    assert all(field["description"].strip() for field in configuration_fields + credential_fields)
    assert all("default" in field for field in configuration_fields)
    assert credential_fields[0]["type"] == "secretString"
    guide = manifest["spec"]["auth"]["guide"]
    assert guide["startURL"] == "https://app.segment.com"
    assert any("Access Management > Tokens > Create Token" in step for step in guide["steps"])
    assert any("https://eu1.api.segmentapis.com" in step for step in guide["steps"])
