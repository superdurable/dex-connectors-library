# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay fixed-clock Go-recorded eSign scenarios through the Python Query."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.esign as esign

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/esign/scenarios.json")
CONNECTION = sdk.ConnectionRef("esign", "esign")
FIXED_TIME = datetime(2026, 11, 16, 12, tzinfo=UTC)
ADAPTER = conformance.PythonOperationAdapter(
    "esign",
    CONNECTION,
    esign,
    lambda http_client, recorder: esign.Client(
        esign.Config(
            max_response_bytes=16 if recorder.scenario["name"] == "getSignFlowStatus-oversize" else 1048576
        ),
        conformance.static_credentials(
            CONNECTION, esign.Credentials("app-123", sdk.SecretString("test-secret"))
        )
        if recorder.scenario["name"] != "getSignFlowStatus-missing-secret"
        else sdk.StaticCredentialProvider({}),
        http_client,
        current_time=lambda: FIXED_TIME,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match exact signed requests, outputs, branches, and retries against Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
