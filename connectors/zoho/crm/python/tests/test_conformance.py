# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Zoho CRM provider scenarios through Python."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.zoho_crm as crm

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/zoho/crm/scenarios.json")
CONNECTION = sdk.ConnectionRef("zoho", "zoho-crm")
ADAPTER = conformance.PythonOperationAdapter(
    "zoho-crm",
    CONNECTION,
    crm,
    lambda client, _: crm.Client(
        crm.Config(),
        conformance.static_credentials(
            CONNECTION,
            crm.Credentials(
                auth_method_id="zoho-us-oauth",
                oauth_client_id="synthetic-client",
                oauth_client_secret=sdk.SecretString("synthetic-secret"),
                access_token=sdk.SecretString("synthetic-access"),
                refresh_token=sdk.SecretString("synthetic-refresh"),
                api_domain="https://www.zohoapis.com",
            ),
        ),
        client,
        api_base_url="http://127.0.0.1:12345/crm/v8",
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Check exact request, branch, failure, and SDK-encoded output parity."""
    await conformance.replay(scenario, ADAPTER.invoke, "/crm/v8")
