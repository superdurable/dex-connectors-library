# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Heap exchanges through the Python Mutation."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.heap as heap

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/heap/scenarios.json")
CONNECTION = sdk.ConnectionRef("heap", "heap")
ADAPTER = conformance.PythonOperationAdapter(
    "heap", CONNECTION, heap,
    lambda client, _: heap.Client(
        heap.Config(app_id="12345", base_url="http://127.0.0.1:8848", max_response_bytes=8), client
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Check provider requests and operation outcomes against Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
