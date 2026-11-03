# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Hacker News provider exchanges."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.hackernews as hackernews
from dex_connectors.hackernews.client import ListStoryIDsOperation

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/hackernews/scenarios.json")
CONNECTION = sdk.ConnectionRef("hackernews", "public")
ADAPTER = conformance.PythonOperationAdapter(
    "hackernews", CONNECTION, hackernews,
    lambda http_client, recorder: hackernews.Client(
        hackernews.Config(base_url="http://127.0.0.1/v0", max_response_bytes=300),
        http_client,
        now=conformance.scenario_clock(recorder.scenario),
    ),
    operation_types={"listStoryIDs": ListStoryIDsOperation},
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare requests, retries, branches, and codec outputs with Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
