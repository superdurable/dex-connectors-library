# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Cohere behavior through the existing Python port."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.cohere as cohere

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/cohere/scenarios.json")
CONNECTION = sdk.ConnectionRef("cohere", "cohere")
ADAPTER = conformance.PythonOperationAdapter(
    "cohere", CONNECTION, cohere,
    lambda client, _: cohere.Client(
        cohere.Config(base_url="http://127.0.0.1"),
        conformance.static_credentials(CONNECTION, cohere.Credentials(sdk.SecretString("test-secret-key"))),
        client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python provider requests and outcomes with the Go recording."""
    await conformance.replay(scenario, ADAPTER.invoke)
