# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Quanshi provider scenarios."""

import sys
from datetime import datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import httpx
import pytest

import dex_connectors.quanshi as connector

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/quanshi/scenarios.json")
CONNECTION = sdk.ConnectionRef("quanshi", "quanshi")


def create_client(http_client: httpx.AsyncClient, recorder: conformance.ExchangeRecorder) -> connector.Client:
    """Construct a credential-safe Quanshi client for each Go recording."""
    invalid = recorder.scenario["name"].endswith("-defect")
    credentials = connector.Credentials(
        app_id=sdk.SecretString("" if invalid else "REDACTED_APP_ID"),
        app_secret=sdk.SecretString("" if invalid else "REDACTED_APP_SECRET"),
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


ADAPTER = conformance.PythonOperationAdapter("quanshi", CONNECTION, connector, create_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare exact provider requests, branches, retries, and outputs with Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
