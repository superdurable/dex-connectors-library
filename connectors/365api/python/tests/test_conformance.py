# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded 365 API model-list scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors._365api as hby365

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/365api/scenarios.json")
CONNECTION = sdk.ConnectionRef("hby365", "hby365")
ADAPTER = conformance.PythonOperationAdapter(
    "hby365",
    CONNECTION,
    hby365,
    lambda http_client, recorder: hby365.Client(
        hby365.Config(
            max_response_bytes=16 if recorder.scenario["name"] == "listModels-oversized" else 1048576
        ),
        conformance.static_credentials(CONNECTION, hby365.Credentials(sdk.SecretString("test-key")))
        if recorder.scenario["name"] != "listModels-defect"
        else sdk.StaticCredentialProvider({}),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare model-list requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
