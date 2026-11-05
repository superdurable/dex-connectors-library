# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Zoho CRM provider decisions through the Python SDK."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.zoho_crm as crm

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/zoho/crm/scenarios.json")
CONNECTION = sdk.ConnectionRef("zoho", "zoho-crm-test")


def _client(client: object, recorder: object) -> crm.Client:
    return crm.Client(
        crm.Config(),
        conformance.static_credentials(CONNECTION, crm.Credentials(
            auth_method_id="zoho-us-oauth",
            oauth_client_id="zoho-client-id",
            oauth_client_secret=sdk.SecretString("zoho-client-secret"),
            access_token=sdk.SecretString("1000.zohoCRMTestAccessToken0123456789"),
            refresh_token=sdk.SecretString("1000.zohoCRMTestRefreshToken"),
            api_domain="https://www.zohoapis.com",
        )),
        client,
        api_base_url="http://127.0.0.1:12345/crm/v8",
    )


ADAPTER = conformance.PythonOperationAdapter(
    "zoho-crm", CONNECTION, crm, _client,
    {
        "findRecords": crm.FindRecordsOperation,
        "getRecord": crm.GetRecordOperation,
        "listModifiedRecords": crm.ListModifiedRecordsOperation,
        "upsertRecord": crm.UpsertRecordOperation,
        "updateRecord": crm.UpdateRecordOperation,
        "listModuleFields": crm.ListModuleFieldsOperation,
    },
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare every request, attempt, branch, output, and failure kind."""
    await conformance.replay(scenario, ADAPTER.invoke, "/crm/v8")
