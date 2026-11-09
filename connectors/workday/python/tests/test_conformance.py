# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded Workday provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.workday as workday

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/workday/scenarios.json")
CONNECTION = sdk.ConnectionRef("workday", "workday")
ADAPTER = conformance.PythonOperationAdapter(
    "workday",
    CONNECTION,
    workday,
    lambda http_client, _: workday.Client(
        workday.Config(
            base_url="http://127.0.0.1:8848/api/common/v1/demo",
            authorization_endpoint="http://127.0.0.1:8848/authorize",
            token_endpoint="http://127.0.0.1:8848/token",
            scopes=("Staffing",),
        ),
        conformance.static_credentials(
            CONNECTION,
            workday.Credentials(
                "synthetic-client-id",
                sdk.SecretString("synthetic-client-secret"),
                sdk.SecretString("test-workday-token"),
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
