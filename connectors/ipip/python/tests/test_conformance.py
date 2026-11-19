# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded IPIP IP location scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.ipip as ipip

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/ipip/scenarios.json")
CONNECTION = sdk.ConnectionRef("ipip", "ipip")
ADAPTER = conformance.PythonOperationAdapter(
    "ipip",
    CONNECTION,
    ipip,
    lambda http_client, recorder: ipip.Client(
        ipip.Config(),
        conformance.static_credentials(CONNECTION, ipip.Credentials(sdk.SecretString("test-secret"))),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare IP location requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
