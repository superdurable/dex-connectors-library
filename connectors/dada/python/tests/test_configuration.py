# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Audit the fields visible for the runnable Dada status Flow in Dex Web."""

from pathlib import Path

import yaml
from dex_connectors.sdk import SecretString

from dex_connectors.dada import Credentials
from dex_connectors.dada.client import _sign_request

MANIFEST = Path(__file__).parents[2] / "connector.yaml"


def test_status_connection_configuration_guidance() -> None:
    """Authorization and connection fields explain origin, format, and blank defaults."""
    spec = yaml.safe_load(MANIFEST.read_text())["spec"]
    assert spec["auth"]["guide"]["startURL"] == "https://newopen.imdada.cn"
    assert {field["name"] for field in spec["auth"]["fields"]} == {
        "appKey", "appSecret", "sourceID"
    }
    for field in spec["auth"]["fields"]:
        assert "https://newopen.imdada.cn" in field["description"]
        assert "Blank" in field["description"] or "blank" in field["description"]
    secret_field = next(
        field for field in spec["auth"]["fields"] if field["name"] == "appSecret"
    )
    assert secret_field["type"] == "secretString"
    fields = spec["configuration"]["fields"]
    assert {field["name"] for field in fields} == {"baseUrl", "timeout", "maxResponseBytes"}
    for field in fields:
        assert field["default"] is not None
        assert "Blank" in field["description"]
    assert spec["operations"][0].get("configurationUI") is None
    credentials = Credentials("123456", SecretString("abcdefg123"), "73753")
    assert "abcdefg123" not in repr(credentials)


def test_official_signature_vector() -> None:
    """Match the official Dada uppercase MD5 example from the Go provider test."""
    assert _sign_request(
        "abcdefg123", "123456", '{"order_id":"20170301000001"}', "73753", "1488363493"
    ) == "71CEE2D6544277940D8C72979E71D0E1"
