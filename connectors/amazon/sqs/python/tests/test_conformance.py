# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded SQS provider exchanges through the Python connector."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.amazon_sqs as sqs

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/amazon/sqs/scenarios.json")
CONNECTION = sdk.ConnectionRef("amazon-sqs", "sqs-test")
ADAPTER = PythonOperationAdapter(
    "amazon-sqs",
    CONNECTION,
    sqs,
    lambda http_client, _: sqs.Client(
        sqs.Config(
            endpoint="http://127.0.0.1:12345",
            default_queue_url="http://127.0.0.1:12345/123456789012/orders",
            region="eu-west-1",
        ),
        sdk.StaticCredentialProvider({
            CONNECTION: sqs.Credentials(
                sdk.SecretString("AKIASQSCONNECTORTEST"),
                sdk.SecretString("sqs-connector-test/secret+value"),
                sdk.SecretString(""),
            ),
        }),
        http_client,
        now=lambda: datetime(2026, 10, 30, tzinfo=UTC),
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare requests, retries, branches, and SDK encoded outputs to Go."""
    await replay(scenario, ADAPTER.invoke)
