# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Datadog provider scenarios through the Python port."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.datadog as datadog

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/datadog/scenarios.json")
CONNECTION = sdk.ConnectionRef("datadog", "datadog")
ADAPTER = conformance.PythonOperationAdapter(
    "datadog", CONNECTION, datadog,
    lambda http_client, _: datadog.Client(
        datadog.Config(base_url="http://localhost"),
        conformance.static_credentials(
            CONNECTION,
            datadog.Credentials(sdk.SecretString("test-apikey"), sdk.SecretString("test-applicationkey")),
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match Go requests and decisions without live provider credentials."""
    await conformance.replay(scenario, ADAPTER.invoke)
