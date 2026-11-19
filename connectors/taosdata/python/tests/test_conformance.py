# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded TDengine Cloud provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest
from dex_connectors.sdk import SecretString

import dex_connectors.taosdata as connector

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/taosdata/scenarios.json")
CONNECTION = sdk.ConnectionRef("taosdata", "taosdata")
ADAPTER = conformance.PythonOperationAdapter(
    "taosdata",
    CONNECTION,
    connector,
    lambda http_client, recorder: connector.Client(
        connector.Config(),
        conformance.static_credentials(
            CONNECTION,
            connector.Credentials(
                SecretString(
                    "" if recorder.scenario["name"].endswith("-defect") else "https://cloud.example.test"
                ),
                SecretString("secret-cloud-token"),
            ),
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare provider requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
