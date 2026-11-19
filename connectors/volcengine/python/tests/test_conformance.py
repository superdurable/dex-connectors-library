# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Volcengine provider scenarios."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest
from dex_connectors.sdk import SecretString

import dex_connectors.volcengine as connector

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/volcengine/scenarios.json")
CONNECTION = sdk.ConnectionRef("volcengine", "volcengine")
ADAPTER = conformance.PythonOperationAdapter(
    "volcengine",
    CONNECTION,
    connector,
    lambda http_client, recorder: connector.Client(
        connector.Config(base_url="http://127.0.0.1:12345"),
        conformance.static_credentials(
            CONNECTION,
            connector.Credentials(
                SecretString("" if recorder.scenario["name"].endswith("-defect") else "AKLTtest-access-key"),
                SecretString("test-secret-access-key"),
            ),
        ),
        http_client,
        now=lambda: datetime(2025, 3, 29, 18, 9, 37, tzinfo=UTC),
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare provider requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
