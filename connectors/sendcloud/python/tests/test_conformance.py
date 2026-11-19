# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded SendCloud provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.sendcloud as sendcloud

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/sendcloud/scenarios.json")
CONNECTION = sdk.ConnectionRef("sendcloud", "sendcloud")
ADAPTER = conformance.PythonOperationAdapter(
    "sendcloud",
    CONNECTION,
    sendcloud,
    lambda http_client, recorder: sendcloud.Client(
        sendcloud.Config(
            max_response_bytes=16 if recorder.scenario["name"] == "getAccountBalance-oversized" else 262144
        ),
        conformance.static_credentials(
            CONNECTION,
            sendcloud.Credentials(sdk.SecretString("test-api-user"), sdk.SecretString("test-api-key")),
        )
        if recorder.scenario["name"] != "getAccountBalance-defect"
        else sdk.StaticCredentialProvider({}),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match exact Go requests, branches, outputs, and retry decisions."""
    await conformance.replay(scenario, ADAPTER.invoke)
