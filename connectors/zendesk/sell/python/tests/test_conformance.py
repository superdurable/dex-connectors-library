# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded ZendeskSell provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.zendesk_sell as zendesk_sell

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/zendesk/sell/scenarios.json")
CONNECTION = sdk.ConnectionRef("zendesk-sell", "zendesk-sell")
TEST_CREDENTIALS = zendesk_sell.Credentials(sdk.SecretString("test-token"))
ADAPTER = conformance.PythonOperationAdapter(
    "zendesk-sell",
    CONNECTION,
    zendesk_sell,
    lambda http_client, recorder: zendesk_sell.Client(
        zendesk_sell.Config(base_url="http://127.0.0.1:8848/v2"),
        conformance.static_credentials(CONNECTION, TEST_CREDENTIALS)
        if recorder.scenario["name"] != "listLeads-defect"
        else conformance.static_credentials(
            sdk.ConnectionRef("zendesk-sell", "missing"), TEST_CREDENTIALS
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke, path_prefix="/v2")
