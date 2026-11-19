# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay exact Go-recorded JD Logistics provider requests and decisions."""

import sys
from datetime import datetime, timedelta, timezone
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.jdlogistics as connector

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/jdlogistics/scenarios.json")
CONNECTION = sdk.ConnectionRef("jdlogistics", "jdlogistics")
ADAPTER = conformance.PythonOperationAdapter(
    "jdlogistics",
    CONNECTION,
    connector,
    lambda http_client, recorder: connector.Client(
        connector.Config(
            base_url="http://127.0.0.1:8848",
            max_response_bytes=5 if recorder.scenario["name"] == "queryWaybillTrace-oversized" else 65536,
        ),
        conformance.static_credentials(
            CONNECTION,
            connector.Credentials(
                "app123",
                sdk.SecretString("secretKey"),
                sdk.SecretString("token123"),
                "010K123",
            ),
        ),
        http_client,
        clock=lambda: datetime(2025, 1, 2, 3, 4, 5, tzinfo=timezone(timedelta(hours=8))),
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare complete requests and SDK-encoded outcomes with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
