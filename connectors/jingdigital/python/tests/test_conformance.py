# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded JINGdigital tag scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.jingdigital as jingdigital

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/jingdigital/scenarios.json")
CONNECTION = sdk.ConnectionRef("jingdigital", "jingdigital")
ADAPTER = conformance.PythonOperationAdapter(
    "jingdigital",
    CONNECTION,
    jingdigital,
    lambda http_client, recorder: jingdigital.Client(
        jingdigital.Config(),
        conformance.static_credentials(
            CONNECTION, jingdigital.Credentials(sdk.SecretString("app-test"), sdk.SecretString("secret-test"))
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare tag requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
