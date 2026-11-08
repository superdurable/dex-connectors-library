# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded Freshsales provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.freshworks_freshsales as freshsales

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/freshworks/freshsales/scenarios.json")
CONNECTION = sdk.ConnectionRef("freshsales", "freshsales")
TEST_CREDENTIALS = freshsales.Credentials(sdk.SecretString("test-token"))
ADAPTER = conformance.PythonOperationAdapter(
    "freshsales",
    CONNECTION,
    freshsales,
    lambda http_client, recorder: freshsales.Client(
        freshsales.Config(base_url="http://127.0.0.1:8848/crm/sales/api"),
        conformance.static_credentials(CONNECTION, TEST_CREDENTIALS)
        if recorder.scenario["name"] != "listContactViews-defect"
        else conformance.static_credentials(
            sdk.ConnectionRef("freshsales", "missing"), TEST_CREDENTIALS
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke, path_prefix="/crm/sales/api")
