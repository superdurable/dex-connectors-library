# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Notion responses through the Python provider operations."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.notion as notion

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/notion/scenarios.json")
CONNECTION = sdk.ConnectionRef("notion", "notion-test")
ADAPTER = PythonOperationAdapter(
    "notion",
    CONNECTION,
    notion,
    lambda transport, _: notion.Client(
        notion.Config(endpoint="http://127.0.0.1:12345"),
        sdk.StaticCredentialProvider(
            {CONNECTION: notion.Credentials(sdk.SecretString("ntn_test_token_value"))}
        ),
        transport,
    ),
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match exact requests and SDK encoded operation results against Go."""
    await replay(scenario, ADAPTER.invoke, "/v1")
