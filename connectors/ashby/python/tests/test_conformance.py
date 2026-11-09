# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded Ashby provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.ashby as ashby

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/ashby/scenarios.json")
CONNECTION = sdk.ConnectionRef("ashby", "ashby")
TEST_CREDENTIALS = ashby.Credentials(sdk.SecretString("sample-ashby-api-key"))
ADAPTER = conformance.PythonOperationAdapter(
    "ashby",
    CONNECTION,
    ashby,
    lambda http_client, _recorder: ashby.Client(
        ashby.Config(base_url="http://127.0.0.1:8848/api"),
        conformance.static_credentials(CONNECTION, TEST_CREDENTIALS),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke, path_prefix="/api")
