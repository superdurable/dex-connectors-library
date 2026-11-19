# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded geovisearth requests and outcomes."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.geovisearth as geovisearth

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/geovisearth/scenarios.json")
CONNECTION = sdk.ConnectionRef("geovisearth", "geovisearth")
ADAPTER = conformance.PythonOperationAdapter(
    "geovisearth",
    CONNECTION,
    geovisearth,
    lambda http_client, recorder: geovisearth.Client(
        geovisearth.Config(),
        conformance.static_credentials(
            CONNECTION, geovisearth.Credentials(sdk.SecretString("private-test-token"))
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match exact Go requests, branches, outputs, and retry decisions."""
    await conformance.replay(scenario, ADAPTER.invoke)
