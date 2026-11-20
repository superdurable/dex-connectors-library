# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Winit warehouse scenarios."""

import sys
from datetime import datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import httpx
import pytest

import dex_connectors.winit as connector

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/winit/scenarios.json")
CONNECTION = sdk.ConnectionRef("winit", "winit")


def create_client(http_client: httpx.AsyncClient, recorder: conformance.ExchangeRecorder) -> connector.Client:
    """Use inert fixture credentials and deterministic signing time."""
    scenario = recorder.scenario
    credentials = connector.Credentials(
        sdk.SecretString("REDACTED_APP_KEY"),
        sdk.SecretString("REDACTED_TOKEN"),
        sdk.SecretString("REDACTED_CLIENT_ID"),
        sdk.SecretString("REDACTED_CLIENT_SECRET"),
    )
    if scenario["name"] == "listWarehouses-defect":
        credentials = connector.Credentials(*(sdk.SecretString("") for _ in range(4)))
    return connector.Client(
        connector.Config(
            platform="OWNERERP",
            max_response_bytes=8 if scenario["name"] == "listWarehouses-oversized" else 2097152,
        ),
        conformance.static_credentials(CONNECTION, credentials),
        http_client,
        now=datetime.fromisoformat(str(scenario["fixedClock"]).replace("Z", "+00:00")),
    )


ADAPTER = conformance.PythonOperationAdapter("winit", CONNECTION, connector, create_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare request signature, branch, retry, and output with Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
