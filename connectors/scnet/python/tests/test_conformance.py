# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded SCNet provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.scnet as connector

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/scnet/scenarios.json")
CONNECTION = sdk.ConnectionRef("scnet", "scnet")


def make_client(client: object, recorder: conformance.ExchangeRecorder) -> connector.Client:
    """Build the Query with synthetic credentials for the recorded scenario."""
    secret_values = (
        (sdk.SecretString(""),)
        if recorder.scenario["name"] == "listModels-defect"
        else (sdk.SecretString("private-test-key"),)
    )
    return connector.Client(
        connector.Config(),
        conformance.static_credentials(CONNECTION, connector.Credentials(*secret_values)),
        client,
    )


ADAPTER = conformance.PythonOperationAdapter("scnet", CONNECTION, connector, make_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare provider requests and decisions with Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
