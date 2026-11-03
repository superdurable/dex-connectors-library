# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Coda provider scenarios through the Python port."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.coda as coda

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/coda/scenarios.json")
CONNECTION = sdk.ConnectionRef("coda", "coda")
ADAPTER = conformance.PythonOperationAdapter(
    "coda", CONNECTION, coda,
    lambda client, _: coda.Client(
        coda.Config(base_url="http://127.0.0.1:12345/apis/v1"),
        conformance.static_credentials(CONNECTION, coda.Credentials(sdk.SecretString("test-token"))),
        client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match Go request bytes and classified outcomes."""
    await conformance.replay(scenario, ADAPTER.invoke)
