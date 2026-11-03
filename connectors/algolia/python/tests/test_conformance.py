# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Algolia provider scenarios through the Python package."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.algolia as algolia

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/algolia/scenarios.json")
CONNECTION = sdk.ConnectionRef("algolia", "algolia")
ADAPTER = PythonOperationAdapter(
    "algolia", CONNECTION, algolia,
    lambda client, _: algolia.Client(
        algolia.Config(base_url="http://127.0.0.1:12345/1"),
        sdk.StaticCredentialProvider({
            CONNECTION: algolia.Credentials("TESTAPP", sdk.SecretString("algolia-test-key"))
        }),
        client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Check Python request and outcome parity with Go recordings."""
    await replay(scenario, ADAPTER.invoke, "/1")
