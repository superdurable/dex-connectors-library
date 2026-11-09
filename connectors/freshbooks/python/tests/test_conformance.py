# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded FreshBooks provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.freshbooks as freshbooks

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/freshbooks/scenarios.json")
CONNECTION = sdk.ConnectionRef("freshbooks", "freshbooks")
ADAPTER = conformance.PythonOperationAdapter(
    "freshbooks",
    CONNECTION,
    freshbooks,
    lambda http_client, _: freshbooks.Client(
        freshbooks.Config(redirect_uri="https://dex.example/callback", base_url="http://127.0.0.1:8848"),
        conformance.static_credentials(
            CONNECTION,
            freshbooks.Credentials(
                "synthetic-client-id",
                sdk.SecretString("synthetic-client-secret"),
                sdk.SecretString("test-freshbooks-token"),
                sdk.SecretString("synthetic-refresh-token"),
            ),
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke, path_prefix="")
