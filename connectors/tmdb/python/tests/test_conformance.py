# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded TMDB scenarios through the Python package."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.tmdb as tmdb

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/tmdb/scenarios.json")
CONNECTION = sdk.ConnectionRef("tmdb", "tmdb")
ADAPTER = conformance.PythonOperationAdapter(
    "tmdb", CONNECTION, tmdb,
    lambda client, _: tmdb.Client(
        tmdb.Config(base_url="http://127.0.0.1:8848/3"),
        conformance.static_credentials(CONNECTION, tmdb.Credentials(sdk.SecretString("test-secret-token"))),
        client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Check provider requests and outcomes against the Go recording."""
    await conformance.replay(scenario, ADAPTER.invoke)
