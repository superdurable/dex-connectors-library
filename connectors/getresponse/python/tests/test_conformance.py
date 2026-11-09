# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded GetResponse provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.getresponse as getresponse

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/getresponse/scenarios.json")
CONNECTION = sdk.ConnectionRef("getresponse", "getresponse")
ADAPTER = conformance.PythonOperationAdapter(
    "getresponse",
    CONNECTION,
    getresponse,
    lambda http_client, _: getresponse.Client(
        getresponse.Config(base_url="http://127.0.0.1:8848/v3"),
        conformance.static_credentials(
            CONNECTION, getresponse.Credentials(sdk.SecretString("test-token"))
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke, path_prefix="/v3")
