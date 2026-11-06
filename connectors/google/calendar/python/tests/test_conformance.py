# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Google Calendar provider scenarios."""

import sys
from copy import deepcopy
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.google_calendar as calendar

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import (  # noqa: E402
    ExchangeRecorder,
    HeartbeatContext,
    PythonOperationAdapter,
    load_scenarios,
    replay,
)

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


async def test_duplicate_create_receipt_comes_from_read_back() -> None:
    """Keep the provider request ID from the confirming GET after a POST 409."""
    scenario = deepcopy(next(item for item in SCENARIOS if item["name"] == "createEvent-duplicate-readback"))
    scenario["exchanges"][1]["headers"] = {"X-Goog-Request-Id": "post-request"}
    scenario["exchanges"][2]["headers"] = {"X-Goog-Request-Id": "read-request"}
    recorder = ExchangeRecorder(scenario, scenario["callId"], "/calendar/v3")
    context = HeartbeatContext(scenario.get("priorHeartbeat"), scenario.get("fixedClock"), scenario["name"])
    attempt = await ADAPTER.invoke(scenario, recorder, context)
    recorder.assert_complete()
    assert attempt.receipt is not None
    assert attempt.receipt.provider_request_id == "read-request"
