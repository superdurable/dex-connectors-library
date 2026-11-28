# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Siliconflow provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.siliconflow as connector

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/siliconflow/scenarios.json")
CONNECTION = sdk.ConnectionRef("siliconflow", "siliconflow")


def make_client(client: object, recorder: conformance.ExchangeRecorder) -> connector.Client:
    """Build the Query using synthetic recorded credentials."""
    name = recorder.scenario["name"]
    config = (
        connector.Config(max_response_bytes=16)
        if recorder.scenario["name"] == "listModels-oversized"
        else connector.Config()
    )
    credentials = connector.Credentials(
        sdk.SecretString("x" * 513 if name.endswith("-defect") else "test-siliconflow-key-123")
    )
    return connector.Client(config, conformance.static_credentials(CONNECTION, credentials), client)


ADAPTER = conformance.PythonOperationAdapter("siliconflow", CONNECTION, connector, make_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare provider requests and decisions with Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
