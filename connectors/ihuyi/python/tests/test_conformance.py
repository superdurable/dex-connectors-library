# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Ihuyi IP location scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.ihuyi as ihuyi

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/ihuyi/scenarios.json")
CONNECTION = sdk.ConnectionRef("ihuyi", "ihuyi")
ADAPTER = conformance.PythonOperationAdapter(
    "ihuyi",
    CONNECTION,
    ihuyi,
    lambda http_client, recorder: ihuyi.Client(
        ihuyi.Config(max_response_bytes=16 if recorder.scenario["name"].endswith("oversized") else 65536),
        conformance.static_credentials(
            CONNECTION, ihuyi.Credentials("test-account", sdk.SecretString("private-test-key"))
        ),
        http_client,
        lambda: conformance.HeartbeatContext(fixed_clock=recorder.scenario.get("fixedClock")).fixed_clock,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare IP location requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
