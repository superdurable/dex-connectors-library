# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded AWeber provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.aweber as aweber

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/aweber/scenarios.json")
CONNECTION = sdk.ConnectionRef("aweber", "aweber")
ADAPTER = conformance.PythonOperationAdapter(
    "aweber",
    CONNECTION,
    aweber,
    lambda http_client, recorder: aweber.Client(
        aweber.Config(
            base_url="http://127.0.0.1:8848",
            max_response_bytes=16
            if str(recorder.scenario["name"]).startswith("listAccounts-oversized")
            else 1_048_576,
        ),
        conformance.static_credentials(
            CONNECTION,
            aweber.Credentials(
                "synthetic-client-id",
                sdk.SecretString("synthetic-client-secret"),
                sdk.SecretString("test-aweber-token"),
                sdk.SecretString("synthetic-refresh-token"),
            ),
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke, path_prefix="")
