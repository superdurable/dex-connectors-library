# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Wangdian provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.wangdian as connector

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/wangdian/scenarios.json")
CONNECTION = sdk.ConnectionRef("wangdian", "wangdian")


def create_client(http_client: object, recorder: conformance.ExchangeRecorder) -> connector.Client:
    """Build a credential-safe client for each Go recording."""
    scenario_name = recorder.scenario["name"]
    credentials = connector.Credentials(
        sid=sdk.SecretString("" if scenario_name.endswith("-defect") else "test2"),
        app_key=sdk.SecretString("" if scenario_name.endswith("-defect") else "test2-xx"),
        app_secret=sdk.SecretString("" if scenario_name.endswith("-defect") else "12345"),
    )
    config = (
        connector.Config(max_response_bytes=16)
        if scenario_name.endswith("-oversized")
        else connector.Config()
    )
    return connector.Client(
        config, conformance.static_credentials(CONNECTION, credentials), http_client,
        clock_seconds=lambda: 1731510000,
    )


ADAPTER = conformance.PythonOperationAdapter("wangdian", CONNECTION, connector, create_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare exact requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
