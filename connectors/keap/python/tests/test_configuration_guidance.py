# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check the Dex Web fields exposed by the Keap example."""

from pathlib import Path

import yaml


def test_connection_fields_have_guidance_and_safe_defaults() -> None:
    """Every visible connection field explains its source and blank behavior."""
    manifest = yaml.safe_load((Path(__file__).parents[2] / "connector.yaml").read_text())
    auth = manifest["spec"]["auth"]
    assert auth["type"] == "apiKey"
    assert auth["guide"]["startURL"] == "https://developer.keap.com/docs/getting-started"
    assert all("blank" in step.lower() for step in auth["guide"]["steps"])
    assert [(field["name"], field["type"]) for field in auth["fields"]] == [
        ("personalAccessToken", "secretString")
    ]
    assert "API Settings" in auth["fields"][0]["description"]
    assert "Secret" in auth["fields"][0]["description"]
    assert "blank" in auth["fields"][0]["description"]

    fields = manifest["spec"]["configuration"]["fields"]
    assert {field["name"] for field in fields} == {"baseUrl", "timeout", "maxResponseBytes"}
    for field in fields:
        assert field["default"]
        assert "Blank" in field["description"]
        assert str(field["default"]) not in field["description"]
    assert "bytes" in next(field["description"] for field in fields if field["name"] == "maxResponseBytes")
    assert "duration" in next(field["description"] for field in fields if field["name"] == "timeout")
    assert "/rest/v2/contacts" in next(field["description"] for field in fields if field["name"] == "baseUrl")
