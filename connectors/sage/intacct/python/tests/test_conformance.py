# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded Sage Intacct provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.sage_intacct as sageintacct

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/sage/intacct/scenarios.json")
CONNECTION = sdk.ConnectionRef("sage-intacct", "sage-intacct")
ADAPTER = conformance.PythonOperationAdapter(
    "sage-intacct",
    CONNECTION,
    sageintacct,
    lambda http_client, _: sageintacct.Client(
        sageintacct.Config(base_url="http://127.0.0.1:8848"),
        conformance.static_credentials(
            CONNECTION,
            sageintacct.Credentials(
                "synthetic-client-id",
                sdk.SecretString("synthetic-client-secret"),
                sdk.SecretString("test-sage-intacct-token"),
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
