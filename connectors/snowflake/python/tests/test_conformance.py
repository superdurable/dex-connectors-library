# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-verified Snowflake SQL API scenarios through the Python SDK."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.snowflake as snowflake

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/snowflake/scenarios.json")
CONNECTION = sdk.ConnectionRef("snowflake", "warehouse")
ADAPTER = conformance.PythonOperationAdapter(
    "snowflake",
    CONNECTION,
    snowflake,
    lambda client, _: snowflake.Client(
        snowflake.Config(
            account_identifier="myorg-analytics",
            warehouse="REPORTING_WH",
            role="DEX_REPORTING",
            database="ANALYTICS",
            schema="BILLING",
            max_rows=1,
        ),
        conformance.static_credentials(
            CONNECTION,
            snowflake.Credentials(
                auth_method_id="programmatic-access-token",
                programmatic_access_token=sdk.SecretString("pat-secret-value-0123456789"),
            ),
        ),
        client,
        local_provider_url="http://127.0.0.1",
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare each operation's exact provider exchange and decoded outcome with Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
