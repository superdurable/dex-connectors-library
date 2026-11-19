# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded GuGuData text segmentation scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.gugudata as gugudata

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/gugudata/scenarios.json")
CONNECTION = sdk.ConnectionRef("gugudata", "gugudata")
ADAPTER = conformance.PythonOperationAdapter(
    "gugudata",
    CONNECTION,
    gugudata,
    lambda http_client, recorder: gugudata.Client(
        gugudata.Config(),
        conformance.static_credentials(
            CONNECTION, gugudata.Credentials(sdk.SecretString("test-gugudata-app-key"))
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare text segmentation requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
