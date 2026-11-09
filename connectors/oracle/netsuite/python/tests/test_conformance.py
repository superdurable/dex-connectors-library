# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded OracleNetSuite provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.oracle_netsuite as oraclenetsuite

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/oracle/netsuite/scenarios.json")
CONNECTION = sdk.ConnectionRef("oracle-netsuite", "oracle-netsuite")
ADAPTER = conformance.PythonOperationAdapter(
    "oracle-netsuite",
    CONNECTION,
    oraclenetsuite,
    lambda http_client, _: oraclenetsuite.Client(
        oraclenetsuite.Config(
            base_url="http://127.0.0.1:8848",
            authorization_endpoint="http://127.0.0.1:8848/authorize",
            token_endpoint="http://127.0.0.1:8848/token",
        ),
        conformance.static_credentials(
            CONNECTION,
            oraclenetsuite.Credentials(
                "synthetic-client-id",
                sdk.SecretString("synthetic-client-secret"),
                sdk.SecretString("test-oracle-netsuite-token"),
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
