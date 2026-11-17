# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay exact Go-recorded Bilibili provider requests and decisions."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.bilibili as connector

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/bilibili/scenarios.json")
CONNECTION = sdk.ConnectionRef("bilibili", "bilibili")
ADAPTER = conformance.PythonOperationAdapter(
    "bilibili",
    CONNECTION,
    connector,
    lambda http_client, recorder: connector.Client(
        connector.Config(base_url="http://127.0.0.1:8848"),
        conformance.static_credentials(
            CONNECTION,
            connector.Credentials("", sdk.SecretString(""), sdk.SecretString(""))
            if recorder.scenario["name"] == "getAuthorizedUser-defect"
            else connector.Credentials(
                "client123", sdk.SecretString("secret456"), sdk.SecretString("token789")
            ),
        ),
        http_client,
        nonce_bytes=lambda count: bytes(count),
        unix_time=lambda: 1730185575,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare complete requests and SDK-encoded outcomes with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
