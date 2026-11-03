# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Airtable provider exchanges through the Python connector."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.airtable as airtable

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/airtable/scenarios.json")
CONNECTION = sdk.ConnectionRef("airtable", "airtable")
ADAPTER = PythonOperationAdapter(
    "airtable",
    CONNECTION,
    airtable,
    lambda http_client, _: airtable.Client(
        airtable.Config(endpoint="http://127.0.0.1:22080"),
        sdk.StaticCredentialProvider(
            {CONNECTION: airtable.Credentials(sdk.SecretString("patTESTtoken.0123456789abcdef"))}
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare complete requests and SDK-encoded attempt outcomes with the Go recording."""
    await replay(scenario, ADAPTER.invoke)
