# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded Amplitude provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.amplitude as amplitude

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/amplitude/scenarios.json")
CONNECTION = sdk.ConnectionRef("amplitude", "amplitude")
TEST_CREDENTIALS = amplitude.Credentials(
    sdk.SecretString("sample-amplitude-api-key"), sdk.SecretString("sample-amplitude-secret-key")
)
ADAPTER = conformance.PythonOperationAdapter(
    "amplitude",
    CONNECTION,
    amplitude,
    lambda http_client, _recorder: amplitude.Client(
        amplitude.Config(base_url="http://127.0.0.1:8848"),
        conformance.static_credentials(CONNECTION, TEST_CREDENTIALS),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke, path_prefix="")
