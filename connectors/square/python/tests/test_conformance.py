# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Square provider scenarios through the Python package."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.square as square

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/square/scenarios.json")
CONNECTION = sdk.ConnectionRef("square", "square")
ADAPTER = conformance.PythonOperationAdapter(
    "square",
    CONNECTION,
    square,
    lambda client, recorder: square.Client(
        square.Config(base_url="http://127.0.0.1:28711/v2"),
        conformance.static_credentials(
            CONNECTION,
            square.Credentials(
                sdk.SecretString("" if recorder.scenario["name"] == "listPayments-defect" else "test-token")
            ),
        ),
        client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Check provider requests and operation outcomes against Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
