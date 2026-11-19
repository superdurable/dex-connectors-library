# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay exact Go-recorded Sensors Data provider requests and decisions."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.sensorsdata as connector

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/sensorsdata/scenarios.json")
CONNECTION = sdk.ConnectionRef("sensorsdata", "sensorsdata")
ADAPTER = conformance.PythonOperationAdapter(
    "sensorsdata",
    CONNECTION,
    connector,
    lambda http_client, recorder: connector.Client(
        connector.Config(
            base_url="https://family.demo.sensorsdata.cn",
            project_name="production",
            max_response_bytes=5 if recorder.scenario["name"] == "listCampaigns-oversize" else 2097152,
        ),
        conformance.static_credentials(
            CONNECTION,
            connector.Credentials(
                sdk.SecretString(
                    "bad key"
                    if recorder.scenario["name"] == "listCampaigns-invalid-credentials"
                    else "k" * 35,
                )
            ),
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare complete requests and SDK-encoded outcomes with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
