# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded Campaign Monitor provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.campaignmonitor as campaignmonitor

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/campaignmonitor/scenarios.json")
CONNECTION = sdk.ConnectionRef("campaignmonitor", "campaignmonitor")
ADAPTER = conformance.PythonOperationAdapter(
    "campaignmonitor",
    CONNECTION,
    campaignmonitor,
    lambda http_client, recorder: campaignmonitor.Client(
        campaignmonitor.Config(base_url="http://127.0.0.1:8848/api/v3.3"),
        conformance.static_credentials(
            CONNECTION, campaignmonitor.Credentials(sdk.SecretString("test-token"))
        )
        if recorder.scenario["name"] != "listClients-defect"
        else conformance.static_credentials(
            sdk.ConnectionRef("campaignmonitor", "missing"),
            campaignmonitor.Credentials(sdk.SecretString("test-token")),
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke, path_prefix="/api/v3.3")
