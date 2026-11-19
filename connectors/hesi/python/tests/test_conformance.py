# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Hesi department scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.hesi as hesi

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/hesi/scenarios.json")
CONNECTION = sdk.ConnectionRef("hesi", "hesi")
ADAPTER = conformance.PythonOperationAdapter(
    "hesi",
    CONNECTION,
    hesi,
    lambda http_client, recorder: hesi.Client(
        hesi.Config(),
        conformance.static_credentials(
            CONNECTION,
            hesi.Credentials(sdk.SecretString("test-app-key"), sdk.SecretString("test-app-security")),
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Hesi requests, results, and retry decisions with Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
