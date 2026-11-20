# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Useasy provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.useasy as connector

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/useasy/scenarios.json")
CONNECTION = sdk.ConnectionRef("useasy", "useasy")


def create_client(http_client: object, recorder: conformance.ExchangeRecorder) -> connector.Client:
    """Build a credential-safe client for each Go recording."""
    scenario_name = recorder.scenario["name"]
    credentials = connector.Credentials(
        app_id=sdk.SecretString("" if scenario_name.endswith("-defect") else "1011"),
        secret_key=sdk.SecretString("" if scenario_name.endswith("-defect") else "private-test-key"),
    )
    config = (
        connector.Config(max_response_bytes=16)
        if scenario_name.endswith("-oversized")
        else connector.Config()
    )
    return connector.Client(
        config, conformance.static_credentials(CONNECTION, credentials), http_client,
        clock_seconds=lambda: 1731510000, nonce_factory=lambda: "123456",
    )


ADAPTER = conformance.PythonOperationAdapter("useasy", CONNECTION, connector, create_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare exact requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
