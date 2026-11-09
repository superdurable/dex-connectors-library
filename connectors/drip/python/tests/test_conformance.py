# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded Drip provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.drip as drip

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/drip/scenarios.json")
CONNECTION = sdk.ConnectionRef("drip", "drip")
ADAPTER = conformance.PythonOperationAdapter(
    "drip",
    CONNECTION,
    drip,
    lambda http_client, recorder: drip.Client(
        drip.Config(base_url="http://127.0.0.1:8848/v2"),
        conformance.static_credentials(CONNECTION, drip.Credentials(sdk.SecretString("test-token")))
        if recorder.scenario["name"] != "listAccounts-defect"
        else conformance.static_credentials(
            sdk.ConnectionRef("drip", "missing"), drip.Credentials(sdk.SecretString("test-token"))
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke, path_prefix="/v2")
