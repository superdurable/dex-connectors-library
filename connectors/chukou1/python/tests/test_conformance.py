# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Chukou1 provider scenarios through the Python Query."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.chukou1 as chukou1

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/chukou1/scenarios.json")
CONNECTION = sdk.ConnectionRef("chukou1", "chukou1")
ADAPTER = conformance.PythonOperationAdapter(
    "chukou1",
    CONNECTION,
    chukou1,
    lambda http_client, recorder: chukou1.Client(
        chukou1.Config(
            base_url="http://127.0.0.1:12345",
            max_response_bytes=4 if recorder.scenario["name"] == "listWarehouses-oversize" else 1048576,
        ),
        conformance.static_credentials(CONNECTION, chukou1.Credentials(sdk.SecretString("secretTestToken")))
        if recorder.scenario["name"] != "listWarehouses-missing-credentials"
        else sdk.StaticCredentialProvider({}),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match exact requests, outputs, branches, and retry decisions against Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
