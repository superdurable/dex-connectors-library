# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Close provider scenarios through the Python package."""

import sys
from pathlib import Path

import dex_connectors.close as close
import dex_connectors.sdk as sdk
import pytest

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/close/scenarios.json")
CONNECTION = sdk.ConnectionRef("close", "close")
ADAPTER = conformance.PythonOperationAdapter(
    "close", CONNECTION, close,
    lambda client, _: close.Client(
        close.Config(base_url="http://127.0.0.1:21244/api/v1"),
        conformance.static_credentials(
            CONNECTION, close.Credentials(sdk.SecretString("test-secret-key"))),
        client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Check provider requests and operation outcomes against Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
