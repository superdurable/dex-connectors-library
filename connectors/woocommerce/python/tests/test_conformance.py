# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded WooCommerce reads through the existing Python port."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.woocommerce as woocommerce

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/woocommerce/scenarios.json")
CONNECTION = sdk.ConnectionRef("woocommerce", "woocommerce")
ADAPTER = conformance.PythonOperationAdapter(
    "woocommerce", CONNECTION, woocommerce,
    lambda http_client, _: woocommerce.Client(
        woocommerce.Config(store_url="http://localhost/shop"),
        conformance.static_credentials(
            CONNECTION, woocommerce.Credentials(sdk.SecretString("ck_test"), sdk.SecretString("cs_test"))
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python provider requests and outcomes with the Go recording."""
    await conformance.replay(scenario, ADAPTER.invoke, "/shop")
