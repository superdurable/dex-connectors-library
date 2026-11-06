# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Zendesk Support provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.zendesk_support as support

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay, static_credentials  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/zendesk/support/scenarios.json")
CONNECTION = sdk.ConnectionRef("zendesk", "zendesk-support-test")
ADAPTER = PythonOperationAdapter(
    "zendesk-support",
    CONNECTION,
    support,
    lambda http_client, _: support.Client(
        support.Config(subdomain="acme"),
        static_credentials(
            CONNECTION,
            support.Credentials(
                "agent@acme.example.com", sdk.SecretString("zendeskTestToken0123456789abcdef")
            ),
        ),
        http_client,
        api_base_url="http://127.0.0.1:37001/api/v2",
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Require exact Python parity with each Go-recorded exchange and outcome."""
    await replay(scenario, ADAPTER.invoke, "/api/v2")
