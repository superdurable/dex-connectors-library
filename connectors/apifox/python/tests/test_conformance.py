# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Apifox provider scenarios through Python."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.apifox as apifox

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/apifox/scenarios.json")
CONNECTION = sdk.ConnectionRef("apifox", "apifox")
ADAPTER = conformance.PythonOperationAdapter(
    "apifox",
    CONNECTION,
    apifox,
    lambda http_client, _: apifox.Client(
        apifox.Config(),
        conformance.static_credentials(CONNECTION, apifox.Credentials(sdk.SecretString("test-secret"))),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match requests, exact summaries, branches, and retries against Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
