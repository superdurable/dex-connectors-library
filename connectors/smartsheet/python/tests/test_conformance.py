# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Smartsheet provider decisions through the Python package."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.smartsheet as smartsheet

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/smartsheet/scenarios.json")
CONNECTION = sdk.ConnectionRef("smartsheet", "smartsheet")
ADAPTER = conformance.PythonOperationAdapter(
    "smartsheet", CONNECTION, smartsheet,
    lambda client, _: smartsheet.Client(
        smartsheet.Config(base_url="http://127.0.0.1:28590/2.0"),
        conformance.static_credentials(
            CONNECTION, smartsheet.Credentials(sdk.SecretString("test-secret-key"))),
        client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Check Python provider requests and decisions against the Go recording."""
    await conformance.replay(scenario, ADAPTER.invoke)
