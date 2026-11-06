# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Calendly scenarios through the Python SDK."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.calendly as calendly

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/calendly/scenarios.json")
CONNECTION = sdk.ConnectionRef("calendly", "scheduling")
ADAPTER = PythonOperationAdapter(
    "calendly",
    CONNECTION,
    calendly,
    lambda http_client, _: calendly.Client(
        calendly.Config(),
        sdk.StaticCredentialProvider(
            {
                CONNECTION: calendly.Credentials(
                    access_token=sdk.SecretString("SENTINEL-CALENDLY-ACCESS-TOKEN"),
                    webhook_signing_key=sdk.SecretString("SENTINEL-CALENDLY-SIGNING-KEY"),
                )
            }
        ),
        http_client,
        lambda: datetime(2026, 9, 30, 12, tzinfo=UTC),
    ),
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match complete requests, retries, outputs, and provider object IDs."""
    await replay(scenario, ADAPTER.invoke, "")
