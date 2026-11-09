# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded Amazon Redshift provider scenarios."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.amazon_redshift as amazon_redshift

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/amazon/redshift/scenarios.json")
CONNECTION = sdk.ConnectionRef("amazon-redshift", "amazon-redshift")
TEST_CREDENTIALS = amazon_redshift.Credentials(
    sdk.SecretString("AKIAREDSHIFTCONNECTOR"), sdk.SecretString("redshift-test-secret"), sdk.SecretString("")
)
ADAPTER = conformance.PythonOperationAdapter(
    "amazon-redshift",
    CONNECTION,
    amazon_redshift,
    lambda http_client, _recorder: amazon_redshift.Client(
        amazon_redshift.Config(endpoint="http://127.0.0.1:12345", region="eu-west-1"),
        conformance.static_credentials(CONNECTION, TEST_CREDENTIALS),
        http_client,
        now=lambda: datetime(2026, 10, 30, tzinfo=UTC),
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke, path_prefix="")
