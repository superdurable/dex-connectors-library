# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded ApiZero account scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.apizero as apizero

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/apizero/scenarios.json")
CONNECTION = sdk.ConnectionRef("apizero", "apizero")


ADAPTER = conformance.PythonOperationAdapter(
    "apizero",
    CONNECTION,
    apizero,
    lambda http_client, recorder: apizero.Client(
        apizero.Config(
            max_response_bytes=16 if recorder.scenario["name"] == "getAccountInfo-oversized" else 1048576
        ),
        conformance.static_credentials(
            CONNECTION,
            apizero.Credentials(
                sdk.SecretString(
                    "invalid"
                    if recorder.scenario["name"] == "getAccountInfo-defect"
                    else "sk_live_test-secret"
                )
            ),
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare ApiZero requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
