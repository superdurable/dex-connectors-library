# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Chinaz site TDK requests and outcomes."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.chinaz as chinaz

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/chinaz/scenarios.json")
CONNECTION = sdk.ConnectionRef("chinaz", "chinaz")


def make_client(client: object, recorder: conformance.ExchangeRecorder) -> chinaz.Client:
    """Build a provider client with the recorded response limit and synthetic Key."""
    max_response_bytes = 16 if recorder.scenario["name"] == "getSiteTDK-oversized" else 1048576
    api_key = "" if recorder.scenario["name"] == "getSiteTDK-defect" else "secretTestKey"
    return chinaz.Client(
        chinaz.Config(base_url="http://127.0.0.1:8848", max_response_bytes=max_response_bytes),
        conformance.static_credentials(CONNECTION, chinaz.Credentials(sdk.SecretString(api_key))),
        client,
    )


ADAPTER = conformance.PythonOperationAdapter("chinaz", CONNECTION, chinaz, make_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare multipart requests and SDK outcomes with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
