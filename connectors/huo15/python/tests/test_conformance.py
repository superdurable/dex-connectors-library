# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Juxingyi model-list scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.huo15 as huo15

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/huo15/scenarios.json")
CONNECTION = sdk.ConnectionRef("huo15", "huo15")
ADAPTER = conformance.PythonOperationAdapter(
    "huo15",
    CONNECTION,
    huo15,
    lambda http_client, recorder: huo15.Client(
        huo15.Config(
            max_response_bytes=16 if recorder.scenario["name"] == "listModels-oversized" else 1048576
        ),
        conformance.static_credentials(CONNECTION, huo15.Credentials(sdk.SecretString("fsk-test-secret")))
        if recorder.scenario["name"] != "listModels-defect"
        else sdk.StaticCredentialProvider({}),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare model-list requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
