# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded ALAPI provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.alapi as alapi

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/alapi/scenarios.json")
CONNECTION = sdk.ConnectionRef("alapi", "alapi")
ADAPTER = conformance.PythonOperationAdapter(
    "alapi",
    CONNECTION,
    alapi,
    lambda http_client, recorder: alapi.Client(
        alapi.Config(
            max_response_bytes=16 if recorder.scenario["name"] == "lookupIPLocation-oversized" else 65536
        ),
        conformance.static_credentials(CONNECTION, alapi.Credentials(sdk.SecretString("test-token"))),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare ALAPI requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
