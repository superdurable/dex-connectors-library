# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded iP138 token-usage scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.ip138 as ip138

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/ip138/scenarios.json")
CONNECTION = sdk.ConnectionRef("ip138", "ip138")
ADAPTER = conformance.PythonOperationAdapter(
    "ip138",
    CONNECTION,
    ip138,
    lambda http_client, recorder: ip138.Client(
        ip138.Config(),
        conformance.static_credentials(
            CONNECTION, ip138.Credentials(sdk.SecretString("test-secret"))
        )
        if recorder.scenario["name"] != "getTokenUsage-defect"
        else sdk.StaticCredentialProvider({}),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare token-usage requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
