# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Guance dashboard-page scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.guance as guance

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/guance/scenarios.json")
CONNECTION = sdk.ConnectionRef("guance", "guance")
ADAPTER = conformance.PythonOperationAdapter(
    "guance",
    CONNECTION,
    guance,
    lambda http_client, recorder: guance.Client(
        guance.Config(
            max_response_bytes=16 if recorder.scenario["name"] == "listDashboardsPage-oversized" else 1048576
        ),
        conformance.static_credentials(CONNECTION, guance.Credentials(sdk.SecretString("api-secret"))),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare dashboard requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
