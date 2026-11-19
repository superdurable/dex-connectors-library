# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Baidu Maps requests and provider decisions."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.baidu_maps as baidu_maps

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/baidu/maps/scenarios.json")
CONNECTION = sdk.ConnectionRef("baidu-maps", "baidu-maps")
ADAPTER = conformance.PythonOperationAdapter(
    "baidu-maps",
    CONNECTION,
    baidu_maps,
    lambda http_client, _: baidu_maps.Client(
        baidu_maps.Config(base_url="http://127.0.0.1:12345"),
        conformance.static_credentials(
            CONNECTION, baidu_maps.Credentials(sdk.SecretString("test-baidu-server-ak-123"))
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match the exact requests, outputs, branches, and retries against Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
