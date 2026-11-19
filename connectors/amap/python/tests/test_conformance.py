# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Amap requests and outcomes through the Python client."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.amap as amap

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/amap/scenarios.json")
CONNECTION = sdk.ConnectionRef("amap", "amap")
ADAPTER = conformance.PythonOperationAdapter(
    "amap",
    CONNECTION,
    amap,
    lambda client, _: amap.Client(
        amap.Config(base_url="http://127.0.0.1:8848"),
        conformance.static_credentials(CONNECTION, amap.Credentials(sdk.SecretString("test-amap-key-123"))),
        client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare the exact provider request and SDK result with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
