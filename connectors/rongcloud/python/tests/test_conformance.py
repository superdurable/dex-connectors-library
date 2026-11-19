# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded RongCloud provider scenarios."""

import sys
from datetime import datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import httpx
import pytest

import dex_connectors.rongcloud as connector

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/rongcloud/scenarios.json")
CONNECTION = sdk.ConnectionRef("rongcloud", "rongcloud")


def create_client(http_client: httpx.AsyncClient, recorder: conformance.ExchangeRecorder) -> connector.Client:
    """Construct a credential-safe RongCloud client for each Go recording."""
    credentials = connector.Credentials(
        app_key="REDACTED_APP_KEY",
        app_secret=sdk.SecretString("REDACTED_APP_SECRET"),
    )
    random_source = conformance.SyntheticRandomSource(recorder.scenario["randomSeed"])
    fixed_clock = datetime.fromisoformat(recorder.scenario["fixedClock"].replace("Z", "+00:00"))
    return connector.Client(
        connector.Config(),
        conformance.static_credentials(CONNECTION, credentials),
        http_client,
        clock=lambda: fixed_clock,
        random_bytes=random_source.read,
    )


ADAPTER = conformance.PythonOperationAdapter("rongcloud", CONNECTION, connector, create_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare exact provider requests, branches, retries, and outputs with Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
