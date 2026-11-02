# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded Trello provider exchanges through the Python package."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.atlassian_trello as trello

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/atlassian/trello/scenarios.json")
CONNECTION = sdk.ConnectionRef("trello", "trello-test")
ADAPTER = PythonOperationAdapter(
    "trello",
    CONNECTION,
    trello,
    lambda http_client, _: trello.Client(
        trello.Config(endpoint="http://127.0.0.1:12345/1"),
        sdk.StaticCredentialProvider(
            {
                CONNECTION: trello.Credentials(
                    sdk.SecretString("0123456789abcdef0123456789abcdef"),
                    sdk.SecretString("ATTAtrelloTestToken0123456789abcdef"),
                )
            }
        ),
        http_client,
    ),
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare complete requests, retries, branches, outputs, and request counts."""
    await replay(scenario, ADAPTER.invoke)
