# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded PayPal provider scenarios through the Python package."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.paypal as paypal

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/paypal/scenarios.json")
CONNECTION = sdk.ConnectionRef("paypal", "paypal")
ADAPTER = conformance.PythonOperationAdapter(
    "paypal", CONNECTION, paypal,
    lambda client, _: paypal.Client(
        paypal.Config(base_url="http://127.0.0.1:8848"),
        conformance.static_credentials(
            CONNECTION, paypal.Credentials("client-test", sdk.SecretString("secret-test"))),
        client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Check provider requests and operation outcomes against Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
