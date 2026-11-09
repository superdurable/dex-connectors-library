# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded Amazon SES provider scenarios."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.amazon_ses as amazon_ses

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/amazon/ses/scenarios.json")
CONNECTION = sdk.ConnectionRef("amazon-ses", "amazon-ses")
TEST_CREDENTIALS = amazon_ses.Credentials(
    sdk.SecretString("AKIASESCONNECTOR"), sdk.SecretString("ses-test-secret"), sdk.SecretString("")
)
ADAPTER = conformance.PythonOperationAdapter(
    "amazon-ses",
    CONNECTION,
    amazon_ses,
    lambda http_client, _recorder: amazon_ses.Client(
        amazon_ses.Config(endpoint="http://127.0.0.1:12345", region="eu-west-1"),
        conformance.static_credentials(CONNECTION, TEST_CREDENTIALS),
        http_client,
        now=lambda: datetime(2026, 10, 30, tzinfo=UTC),
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke, path_prefix="")
