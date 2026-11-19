# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Qihoo360 provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import httpx
import pytest

import dex_connectors.qihoo360 as connector

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/qihoo360/scenarios.json")
CONNECTION = sdk.ConnectionRef("qihoo360", "qihoo360")


def create_client(http_client: httpx.AsyncClient, recorder: conformance.ExchangeRecorder) -> connector.Client:
    """Build the credential-safe test client."""
    defect = recorder.scenario["name"].endswith("-defect")
    credentials = connector.Credentials(sdk.SecretString("" if defect else "REDACTED_QIHOO_API_KEY"))
    config = connector.Config(
        max_response_bytes=64 if recorder.scenario["name"].endswith("-oversized") else 2097152
    )
    return connector.Client(config, conformance.static_credentials(CONNECTION, credentials), http_client)


ADAPTER = conformance.PythonOperationAdapter("qihoo360", CONNECTION, connector, create_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare exact requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
