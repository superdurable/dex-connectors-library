# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay recorded Go Google Sheets provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.google_spreadsheet as sheets

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/google/spreadsheet/scenarios.json")
CONNECTION = sdk.ConnectionRef("google", "customer-sheet")
ADAPTER = conformance.PythonOperationAdapter(
    "google-sheets",
    CONNECTION,
    sheets,
    lambda transport, _: sheets.Client(
        sheets.Config(endpoint="http://127.0.0.1:12345/v4"),
        conformance.static_credentials(
            CONNECTION,
            sheets.Credentials(
                "client.apps.googleusercontent.com",
                sdk.SecretString("client-secret"),
                sdk.SecretString("sheets-token"),
                sdk.SecretString("refresh-token"),
            ),
        ),
        transport,
    ),
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare complete exchanges, branch outputs, retries, and write checkpoints."""
    await conformance.replay(scenario, ADAPTER.invoke, "/v4")
