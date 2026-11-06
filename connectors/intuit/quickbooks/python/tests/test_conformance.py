# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded QuickBooks provider scenarios through Python invokes."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.intuit_quickbooks as quickbooks

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay, static_credentials  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/intuit/quickbooks/scenarios.json")
CONNECTION = sdk.ConnectionRef("quickbooks", "quickbooks-test")
ADAPTER = PythonOperationAdapter(
    "quickbooks",
    CONNECTION,
    quickbooks,
    lambda client, _: quickbooks.Client(
        quickbooks.Config(realm_id="9341453050298464"),
        static_credentials(
            CONNECTION,
            quickbooks.Credentials(
                "ABtestQuickBooksClientID0123456789",
                sdk.SecretString("client-secret"),
                sdk.SecretString("intuit-test-access-token-0123456789"),
                sdk.SecretString("refresh-token"),
                sdk.SecretString(""),
            ),
        ),
        client,
        local_provider_url="http://127.0.0.1:12345",
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare requests, branches, retries, and outputs with the Go reference."""
    await replay(scenario, ADAPTER.invoke, "/v3/company/9341453050298464")
