# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay recorded Go Todoist provider requests through the Python operations."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.doist_todoist as todoist

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/doist/todoist/scenarios.json")
CONNECTION = sdk.ConnectionRef("todoist", "todoist")
ADAPTER = conformance.PythonOperationAdapter(
    "todoist",
    CONNECTION,
    todoist,
    lambda http_client, _: todoist.Client(
        todoist.Config(base_url="http://localhost/api/v1"),
        conformance.static_credentials(
            CONNECTION, todoist.Credentials(sdk.SecretString("todoist-test-token-never-persist"))
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Require exact Go-recorded requests, outcomes, and retry decisions."""
    await conformance.replay(scenario, ADAPTER.invoke)
