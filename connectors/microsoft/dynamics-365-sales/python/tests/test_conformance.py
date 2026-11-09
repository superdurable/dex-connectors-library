# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded Microsoft Dynamics 365 Sales provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.microsoft_dynamics_365_sales as microsoft_dynamics_365_sales

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(
    ROOT / "conformance/connectors/microsoft/dynamics-365-sales/scenarios.json"
)
CONNECTION = sdk.ConnectionRef("microsoft-dynamics-365-sales", "microsoft-dynamics-365-sales")
TEST_CREDENTIALS = microsoft_dynamics_365_sales.Credentials(sdk.SecretString("test-client-secret"))
ADAPTER = conformance.PythonOperationAdapter(
    "microsoft-dynamics-365-sales",
    CONNECTION,
    microsoft_dynamics_365_sales,
    lambda http_client, _recorder: microsoft_dynamics_365_sales.Client(
        microsoft_dynamics_365_sales.Config(
            environment_url="http://127.0.0.1:12345",
            tenant_id="11111111-1111-4111-8111-111111111111",
            client_id="22222222-2222-4222-8222-222222222222",
        ),
        conformance.static_credentials(CONNECTION, TEST_CREDENTIALS),
        http_client,
        token_endpoint="http://127.0.0.1:12345/token",
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke, path_prefix="")
