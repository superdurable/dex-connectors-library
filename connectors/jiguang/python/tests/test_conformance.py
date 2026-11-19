# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay exact Go-recorded Jiguang provider requests and decisions."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.jiguang as connector

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/jiguang/scenarios.json")
CONNECTION = sdk.ConnectionRef("jiguang", "jiguang")
ADAPTER = conformance.PythonOperationAdapter(
    "jiguang",
    CONNECTION,
    connector,
    lambda http_client, recorder: connector.Client(
        connector.Config(
            base_url="http://127.0.0.1:8848",
            max_response_bytes=5 if recorder.scenario["name"] == "listApplicationTags-oversize" else 65536,
        ),
        conformance.static_credentials(
            CONNECTION,
            connector.Credentials(
                "bad key"
                if recorder.scenario["name"] == "listApplicationTags-invalid-credentials"
                else "7d431e42dfa6a6d693ac2d04",
                sdk.SecretString("5e987ac6d2e04d95a9d8f0d1"),
            ),
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare complete requests and SDK-encoded outcomes with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
