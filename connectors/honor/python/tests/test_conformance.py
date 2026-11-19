# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Honor app ID scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.honor as honor

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/honor/scenarios.json")
CONNECTION = sdk.ConnectionRef("honor", "honor")
ADAPTER = conformance.PythonOperationAdapter(
    "honor",
    CONNECTION,
    honor,
    lambda http_client, recorder: honor.Client(
        honor.Config(),
        conformance.static_credentials(
            CONNECTION, honor.Credentials(sdk.SecretString("test-client"), sdk.SecretString("test-secret"))
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare token/app requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
