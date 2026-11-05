# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay HubSpot's Go-recorded provider scenarios through the Python SDK."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.hubspot as hubspot

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/hubspot/scenarios.json")
CONNECTION = sdk.ConnectionRef("hubspot", "hubspot-crm")
ADAPTER = PythonOperationAdapter(
    "hubspot",
    CONNECTION,
    hubspot,
    lambda client, _: hubspot.Client(
        hubspot.Config(endpoint="http://127.0.0.1:42701"),
        sdk.StaticCredentialProvider(
            {CONNECTION: hubspot.Credentials(access_token=sdk.SecretString("pat-na1-test-token"))}
        ),
        client,
        lambda: datetime(2026, 10, 30, tzinfo=UTC),
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match every provider request and SDK-encoded operation decision."""
    await replay(scenario, ADAPTER.invoke)
