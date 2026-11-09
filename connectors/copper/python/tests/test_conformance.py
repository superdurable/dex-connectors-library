# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded Copper provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.copper as copper

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/copper/scenarios.json")
CONNECTION = sdk.ConnectionRef("copper", "copper")
TEST_CREDENTIALS = copper.Credentials(sdk.SecretString("test-token"), sdk.SecretString("user@example.com"))
ADAPTER = conformance.PythonOperationAdapter(
    "copper",
    CONNECTION,
    copper,
    lambda http_client, recorder: copper.Client(
        copper.Config(base_url="http://127.0.0.1:8848/developer_api/v1"),
        conformance.static_credentials(CONNECTION, TEST_CREDENTIALS)
        if recorder.scenario["name"] != "listLeadStatuses-defect"
        else conformance.static_credentials(
            sdk.ConnectionRef("copper", "missing"), TEST_CREDENTIALS
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke, path_prefix="/developer_api/v1")
