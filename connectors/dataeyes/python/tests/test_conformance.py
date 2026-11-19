# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded DataEyes provider scenarios through the Python Query."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.dataeyes as dataeyes

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/dataeyes/scenarios.json")
CONNECTION = sdk.ConnectionRef("dataeyes", "dataeyes")
ADAPTER = conformance.PythonOperationAdapter(
    "dataeyes",
    CONNECTION,
    dataeyes,
    lambda http_client, recorder: dataeyes.Client(
        dataeyes.Config(
            max_response_bytes=16 if recorder.scenario["name"] == "readWebPage-oversize" else 1048576,
        ),
        conformance.static_credentials(CONNECTION, dataeyes.Credentials(sdk.SecretString("test-secret")))
        if recorder.scenario["name"] != "readWebPage-missing-key"
        else sdk.StaticCredentialProvider({}),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match exact requests, outputs, branches, and retry decisions against Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
