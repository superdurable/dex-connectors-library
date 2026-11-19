# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded IsTero account-status scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.istero as istero

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/istero/scenarios.json")
CONNECTION = sdk.ConnectionRef("istero", "istero")
ADAPTER = conformance.PythonOperationAdapter(
    "istero",
    CONNECTION,
    istero,
    lambda http_client, recorder: istero.Client(
        istero.Config(),
        conformance.static_credentials(
            CONNECTION,
            istero.Credentials(
                sdk.SecretString("private-test-token"),
                "invalid" if recorder.scenario["name"] == "getAccountStatus-defect" else "60066023",
            ),
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare account-status requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
