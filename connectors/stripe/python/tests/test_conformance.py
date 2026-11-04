# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the shared Stripe provider scenarios."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.stripe as stripe

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/stripe/scenarios.json")
CONNECTION = sdk.ConnectionRef("stripe", "payments")
ADAPTER = PythonOperationAdapter(
    "stripe", CONNECTION, stripe,
    lambda client, _: stripe.Client(
        stripe.Config(endpoint="http://127.0.0.1:12345/v1"),
        sdk.StaticCredentialProvider({CONNECTION: stripe.Credentials(sdk.SecretString("sk_test_example"))}),
        client, lambda: datetime(2026, 10, 30, tzinfo=UTC)),
)
@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Check ordered exchanges, retries, outcomes, and Call ID reuse."""
    await replay(scenario, ADAPTER.invoke, "/v1")
