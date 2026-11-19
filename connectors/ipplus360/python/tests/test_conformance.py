# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded IPPlus360 IP location scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.ipplus360 as ipplus360

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/ipplus360/scenarios.json")
CONNECTION = sdk.ConnectionRef("ipplus360", "ipplus360")
ADAPTER = conformance.PythonOperationAdapter(
    "ipplus360",
    CONNECTION,
    ipplus360,
    lambda http_client, recorder: ipplus360.Client(
        ipplus360.Config(),
        conformance.static_credentials(
            CONNECTION, ipplus360.Credentials(sdk.SecretString("private-test-key"))
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare IP location requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
