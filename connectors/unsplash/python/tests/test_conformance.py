# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-verified Unsplash provider scenarios through the Python package."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.unsplash as unsplash

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/unsplash/scenarios.json")
CONNECTION = sdk.ConnectionRef("unsplash", "unsplash")
ADAPTER = conformance.PythonOperationAdapter(
    "unsplash",
    CONNECTION,
    unsplash,
    lambda client, _: unsplash.Client(
        unsplash.Config(base_url="http://localhost"),
        conformance.static_credentials(
            CONNECTION, unsplash.Credentials(sdk.SecretString("unsplash-test-key"))
        ),
        client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Check provider requests and classified outcomes against Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
