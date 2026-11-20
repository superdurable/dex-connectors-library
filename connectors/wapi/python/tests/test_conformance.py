# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Wapi brand-price requests and decisions."""

import sys
from datetime import datetime
from pathlib import Path
from typing import cast

import dex_connectors.sdk as sdk
import httpx
import pytest

import dex_connectors.wapi as connector

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/wapi/scenarios.json")
CONNECTION = sdk.ConnectionRef("wapi", "wapi")


def create_client(http_client: object, recorder: conformance.ExchangeRecorder) -> connector.Client:
    """Use the Go fixture's fixed signing time and inert credentials."""
    invalid = recorder.scenario["name"] == "listVehicleBrands-defect"
    credentials = connector.Credentials(
        app_id="" if invalid else "123",
        app_secret=sdk.SecretString("" if invalid else "REDACTEDAPPSECRET000000000000000"),
    )
    fixed_clock = recorder.scenario.get("fixedClock")
    assert isinstance(fixed_clock, str)
    now = datetime.fromisoformat(fixed_clock.replace("Z", "+00:00"))
    return connector.Client(
        connector.Config(),
        conformance.static_credentials(CONNECTION, credentials),
        cast(httpx.AsyncClient, http_client),
        request_clock=lambda: now,
    )


ADAPTER = conformance.PythonOperationAdapter("wapi", CONNECTION, connector, create_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare exact requests, output, branch, and retry with Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
