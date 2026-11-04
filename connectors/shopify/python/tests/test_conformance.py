# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Shopify order scenarios through the Python port."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.shopify as shopify

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/shopify/scenarios.json")
CONNECTION = sdk.ConnectionRef("shopify", "shopify")
ADAPTER = conformance.PythonOperationAdapter(
    "shopify",
    CONNECTION,
    shopify,
    lambda http_client, _: shopify.Client(
        shopify.Config(shop_subdomain="test-shop"),
        conformance.static_credentials(CONNECTION, shopify.Credentials(sdk.SecretString("test-token"))),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match Go requests and classified outcomes."""
    await conformance.replay(scenario, ADAPTER.invoke, "/admin/api/2026-10")
