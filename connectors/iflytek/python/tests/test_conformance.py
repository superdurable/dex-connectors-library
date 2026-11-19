# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded iFLYTEK Spark chat scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.iflytek as iflytek

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/iflytek/scenarios.json")
CONNECTION = sdk.ConnectionRef("iflytek", "iflytek")
ADAPTER = conformance.PythonOperationAdapter(
    "iflytek",
    CONNECTION,
    iflytek,
    lambda http_client, recorder: iflytek.Client(
        iflytek.Config(),
        conformance.static_credentials(
            CONNECTION, iflytek.Credentials(sdk.SecretString("spark-test-password"))
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare chat requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
