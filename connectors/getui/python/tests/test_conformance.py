# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Getui account balance scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.getui as getui

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/getui/scenarios.json")
CONNECTION = sdk.ConnectionRef("getui", "getui")
ADAPTER = conformance.PythonOperationAdapter(
    "getui",
    CONNECTION,
    getui,
    lambda http_client, recorder: getui.Client(
        getui.Config(max_response_bytes=16 if recorder.scenario["name"].endswith("oversized") else 1048576),
        conformance.static_credentials(
            CONNECTION,
            getui.Credentials(
                "" if recorder.scenario["name"].endswith("defect") else "test-app",
                sdk.SecretString("" if recorder.scenario["name"].endswith("defect") else "test-appkey"),
                sdk.SecretString("" if recorder.scenario["name"].endswith("defect") else "test-mastersecret"),
            ),
        ),
        http_client,
        lambda: conformance.HeartbeatContext(fixed_clock=recorder.scenario["fixedClock"]).fixed_clock,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare requests, outputs, and decisions with the Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
