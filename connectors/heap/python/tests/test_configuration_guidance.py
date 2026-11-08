# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check the Dex Web fields exposed by the Heap examples."""

from pathlib import Path

import yaml


def test_connection_fields_have_complete_guidance() -> None:
    """Every visible field explains where its value comes from and blank behavior."""
    manifest = yaml.safe_load((Path(__file__).parents[2] / "connector.yaml").read_text())
    assert manifest["spec"]["auth"] == {"type": "none", "connectionKind": "none", "fields": []}

    fields = manifest["spec"]["configuration"]["fields"]
    assert {field["name"] for field in fields} == {
        "baseUrl", "appId", "timeout", "maxResponseBytes",
    }
    for field in fields:
        assert "Blank" in field["description"]
        if "default" in field:
            assert field["default"]
            assert str(field["default"]) not in field["description"]

    descriptions = {field["name"]: field["description"] for field in fields}
    assert "https://heapanalytics.com" in descriptions["appId"]
    assert "Projects > the Main Production environment" in descriptions["appId"]
    assert "Non-secret" in descriptions["appId"]
    assert "decimal digits" in descriptions["appId"]
    assert "EU project" in descriptions["baseUrl"]
    assert "duration" in descriptions["timeout"]
    assert "bytes" in descriptions["maxResponseBytes"]
