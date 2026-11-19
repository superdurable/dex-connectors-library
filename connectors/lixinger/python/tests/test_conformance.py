# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay exact Go-recorded Lixinger company scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.lixinger as lixinger

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/lixinger/scenarios.json")
CONNECTION = sdk.ConnectionRef("lixinger", "lixinger")
ADAPTER = conformance.PythonOperationAdapter(
    "lixinger",
    CONNECTION,
    lixinger,
    lambda http_client, recorder: lixinger.Client(
        lixinger.Config(),
        conformance.static_credentials(
            CONNECTION,
            lixinger.Credentials(sdk.SecretString("private-test-token")),
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match exact Go requests, outcomes, and retry decisions."""
    await conformance.replay(scenario, ADAPTER.invoke)
