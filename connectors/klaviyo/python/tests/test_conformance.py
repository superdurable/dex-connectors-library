# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Klaviyo provider scenarios through the Python package."""
import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.klaviyo as klaviyo

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/klaviyo/scenarios.json")
CONNECTION = sdk.ConnectionRef("klaviyo", "klaviyo")
ADAPTER = conformance.PythonOperationAdapter(
    "klaviyo", CONNECTION, klaviyo,
    lambda client, _: klaviyo.Client(
        klaviyo.Config(base_url="http://127.0.0.1:12345/api"),
        conformance.static_credentials(CONNECTION, klaviyo.Credentials(sdk.SecretString("pk_test_key"))),
        client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Check provider requests and operation outcomes against Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
