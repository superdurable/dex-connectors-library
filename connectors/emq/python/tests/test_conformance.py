# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded EMQX Cloud provider scenarios through the Python Query."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.emq as emq

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/emq/scenarios.json")
CONNECTION = sdk.ConnectionRef("emq", "emq")
ADAPTER = conformance.PythonOperationAdapter(
    "emq",
    CONNECTION,
    emq,
    lambda http_client, recorder: emq.Client(
        emq.Config(
            platform_endpoint="http://127.0.0.1:12345/public_api/v1",
            max_response_bytes=64 if recorder.scenario["name"] == "listDeployments-oversize" else 1048576,
        ),
        conformance.static_credentials(
            CONNECTION,
            emq.Credentials(sdk.SecretString("test-platform-key"), sdk.SecretString("test-platform-secret")),
        )
        if recorder.scenario["name"] != "listDeployments-missing-key"
        else sdk.StaticCredentialProvider({}),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match exact requests, outputs, branches, and retry decisions against Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
