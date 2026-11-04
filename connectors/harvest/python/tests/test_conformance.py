# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go Harvest provider scenarios through the Python package."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.harvest as harvest

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/harvest/scenarios.json")
CONNECTION = sdk.ConnectionRef("harvest", "harvest")
ADAPTER = conformance.PythonOperationAdapter(
    "harvest", CONNECTION, harvest,
    lambda client, _: harvest.Client(
        harvest.Config(base_url="http://127.0.0.1:12345/v2", account_id="123"),
        conformance.static_credentials(CONNECTION, harvest.Credentials(sdk.SecretString("test-secret-key"))),
        client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match requests, branches, retries, and output with the Go recording."""
    await conformance.replay(scenario, ADAPTER.invoke)
