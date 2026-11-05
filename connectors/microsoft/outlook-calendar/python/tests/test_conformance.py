# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Graph requests and results through the production Python operations."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.microsoft_outlook_calendar as outlook_calendar
from dex_connectors.microsoft_outlook_calendar.client import (
    CreateEventOperation,
    GetEventOperation,
    ListEventsOperation,
    QueryFreeBusyOperation,
    UpdateEventOperation,
)

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(
    ROOT / "conformance/connectors/microsoft/outlook-calendar/scenarios.json"
)
CONNECTION = sdk.ConnectionRef("microsoft", "outlook-calendar-test")
ADAPTER = conformance.PythonOperationAdapter(
    "outlook-calendar",
    CONNECTION,
    outlook_calendar,
    lambda http_client, _recorder: outlook_calendar.Client(
        outlook_calendar.Config(),
        conformance.static_credentials(
            CONNECTION,
            outlook_calendar.Credentials(
                auth_method_id="microsoft-oauth", access_token=sdk.SecretString("calendar-token")
            ),
        ),
        http_client,
        local_provider_url="http://127.0.0.1:12345/v1.0",
    ),
    {
        "listEvents": ListEventsOperation,
        "getEvent": GetEventOperation,
        "createEvent": CreateEventOperation,
        "updateEvent": UpdateEventOperation,
        "queryFreeBusy": QueryFreeBusyOperation,
    },
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda item: item["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Require exact Go branch, output, request, and retry decisions."""
    await conformance.replay(scenario, ADAPTER.invoke, "/v1.0")
