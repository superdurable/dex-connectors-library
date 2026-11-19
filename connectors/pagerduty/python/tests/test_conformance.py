# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded PagerDuty scenarios through the Python connector."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.pagerduty as pagerduty

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/pagerduty/scenarios.json")
CONNECTION = sdk.ConnectionRef("pagerduty", "pagerduty")
ADAPTER = PythonOperationAdapter(
    "pagerduty",
    CONNECTION,
    pagerduty,
    lambda http_client, _: pagerduty.Client(
        pagerduty.Config(base_url="http://127.0.0.1:26042"),
        sdk.StaticCredentialProvider(
            {CONNECTION: pagerduty.Credentials(sdk.SecretString("test-secret-token"))}
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare exact requests and SDK outcomes with the Go recording."""
    await replay(scenario, ADAPTER.invoke)
