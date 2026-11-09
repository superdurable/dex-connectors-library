# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded MicrosoftAdvertising provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.microsoft_advertising as microsoft_advertising

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/microsoft/advertising/scenarios.json")
CONNECTION = sdk.ConnectionRef("microsoft-advertising", "microsoft-advertising")
ADAPTER = conformance.PythonOperationAdapter(
    "microsoft-advertising",
    CONNECTION,
    microsoft_advertising,
    lambda http_client, _: microsoft_advertising.Client(
        microsoft_advertising.Config(customer_id="73456", base_url="http://127.0.0.1:8848"),
        conformance.static_credentials(
            CONNECTION,
            microsoft_advertising.Credentials(
                "synthetic-client-id",
                sdk.SecretString("synthetic-client-secret"),
                sdk.SecretString("test-developer-token"),
                sdk.SecretString("test-microsoft-advertising-token"),
                sdk.SecretString("synthetic-refresh-token"),
            ),
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke, path_prefix="")
