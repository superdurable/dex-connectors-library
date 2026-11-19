# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Weimob store responses and request details."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import httpx
import pytest

import dex_connectors.weimob as connector

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/weimob/scenarios.json")
CONNECTION = sdk.ConnectionRef("weimob", "weimob")


def create_client(http_client: httpx.AsyncClient, recorder: conformance.ExchangeRecorder) -> connector.Client:
    """Use inert Go fixture credentials and the scenario's response bound."""
    name = recorder.scenario["name"]
    credentials = connector.Credentials(
        "example-client-id",
        sdk.SecretString("REDACTED_CLIENT_SECRET"),
        "bad-shop" if name == "getStoreBasic-defect" else "400019",
    )
    return connector.Client(
        connector.Config(max_response_bytes=5 if name == "getStoreBasic-oversized" else 65536),
        conformance.static_credentials(CONNECTION, credentials),
        http_client,
    )


ADAPTER = conformance.PythonOperationAdapter("weimob", CONNECTION, connector, create_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare exact requests, branches, retries, and outputs against Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
