# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded 51Tracking status-count scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.fiftyonetracking as fiftyonetracking

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/fiftyonetracking/scenarios.json")
CONNECTION = sdk.ConnectionRef("fiftyonetracking", "fiftyonetracking")
ADAPTER = conformance.PythonOperationAdapter(
    "fiftyonetracking",
    CONNECTION,
    fiftyonetracking,
    lambda http_client, recorder: fiftyonetracking.Client(
        fiftyonetracking.Config(
            max_response_bytes=16
            if recorder.scenario["name"] == "getShipmentStatusCounts-oversized"
            else 1048576
        ),
        conformance.static_credentials(
            CONNECTION, fiftyonetracking.Credentials(sdk.SecretString("secretTestKey"))
        )
        if recorder.scenario["name"] != "getShipmentStatusCounts-defect"
        else sdk.StaticCredentialProvider({}),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare status-count requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
