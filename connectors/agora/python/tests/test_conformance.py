# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Agora provider scenarios through the Python operation."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.agora as agora

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/agora/scenarios.json")
CONNECTION = sdk.ConnectionRef("agora", "agora")
ADAPTER = conformance.PythonOperationAdapter(
    "agora",
    CONNECTION,
    agora,
    lambda http_client, recorder: agora.Client(
        agora.Config(base_url="http://127.0.0.1:12345"),
        conformance.static_credentials(
            CONNECTION,
            agora.Credentials(
                "1234567890abcdef1234567890abcdef", "customer-id", sdk.SecretString("customer-secret")
            ),
        ) if recorder.scenario["name"] != "listRunningAgents-defect" else sdk.StaticCredentialProvider({}),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match exact requests, output, branches, and retries against the Go recording."""
    await conformance.replay(scenario, ADAPTER.invoke)
