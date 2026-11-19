# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded APISpace weather scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.apispace as apispace

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/apispace/scenarios.json")
CONNECTION = sdk.ConnectionRef("apispace", "apispace")
ADAPTER = conformance.PythonOperationAdapter(
    "apispace",
    CONNECTION,
    apispace,
    lambda http_client, recorder: apispace.Client(
        apispace.Config(
            max_response_bytes=16 if recorder.scenario["name"] == "getCurrentWeather-oversized" else 1048576
        ),
        conformance.static_credentials(CONNECTION, apispace.Credentials(sdk.SecretString("test-token"))),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare APISpace requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
