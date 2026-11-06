# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Pipedrive exchanges through the Python connector."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.pipedrive as pipedrive

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/pipedrive/scenarios.json")
CONNECTION = sdk.ConnectionRef("pipedrive", "pipedrive-crm")
ADAPTER = PythonOperationAdapter(
    "pipedrive",
    CONNECTION,
    pipedrive,
    lambda transport, recorder: pipedrive.Client(
        pipedrive.Config(endpoint="http://127.0.0.1"),
        sdk.StaticCredentialProvider(
            {
                CONNECTION: pipedrive.Credentials(
                    "api-token", sdk.SecretString("pipedrive-unit-test-api-token")
                ),
            }
        ),
        transport,
        now=lambda: (
            datetime.fromisoformat(recorder.scenario["fixedClock"].replace("Z", "+00:00"))
            if recorder.scenario.get("fixedClock")
            else datetime.fromtimestamp(1, UTC)
        ),
    ),
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare exact requests, SDK results, receipts, and checkpoints with Go."""
    await replay(scenario, ADAPTER.invoke)
