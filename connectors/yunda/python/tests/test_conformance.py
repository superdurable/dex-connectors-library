# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Yunda Express provider scenarios."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import httpx
import pytest

import dex_connectors.yunda as connector

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/yunda/scenarios.json")
CONNECTION = sdk.ConnectionRef("yunda", "yunda")


def create_client(http_client: httpx.AsyncClient, recorder: conformance.ExchangeRecorder) -> connector.Client:
    """Build a credential-safe client for each Go recording."""
    scenario_name = recorder.scenario["name"]
    credentials = connector.Credentials(
        sdk.SecretString("" if scenario_name.endswith("-defect") else "999999"),
        sdk.SecretString("REDACTED_APP_SECRET"),
        sdk.SecretString("REDACTED_PARTNER_ID"),
        sdk.SecretString("REDACTED_BAIMA_SECRET"),
    )
    return connector.Client(
        connector.Config(max_response_bytes=16 if scenario_name.endswith("-oversized") else 1048576),
        conformance.static_credentials(CONNECTION, credentials),
        http_client,
        clock=lambda: datetime.fromtimestamp(1731510000, UTC),
    )


ADAPTER = conformance.PythonOperationAdapter("yunda", CONNECTION, connector, create_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare exact requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
