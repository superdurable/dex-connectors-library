# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Google Calendar provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.google_calendar as calendar

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/google/calendar/scenarios.json")
CONNECTION = sdk.ConnectionRef("google", "google-calendar-test")
ADAPTER = PythonOperationAdapter(
    "google-calendar",
    CONNECTION,
    calendar,
    lambda http_client, _: calendar.Client(
        calendar.Config(endpoint="http://127.0.0.1/calendar/v3"),
        sdk.StaticCredentialProvider(
            {CONNECTION: calendar.Credentials(access_token=sdk.SecretString("calendar-token"))}
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Check provider requests and SDK-coded outcomes against Go."""
    await replay(scenario, ADAPTER.invoke, "/calendar/v3")
