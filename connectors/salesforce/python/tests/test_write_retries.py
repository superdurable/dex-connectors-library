# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Repeat-safe Salesforce writes retry an HTTP status that does not prove a refusal."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import httpx
import pytest

import dex_connectors.salesforce as salesforce

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

CONNECTION = sdk.ConnectionRef("salesforce", "salesforce-crm")


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [409, 412, 302, 418])
async def test_writes_retry_a_status_that_does_not_prove_a_refusal(status: int) -> None:
    def respond(request: httpx.Request) -> httpx.Response:
        return httpx.Response(status, json=[{"message": "no", "errorCode": "UNKNOWN"}])

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as http_client:
        client = salesforce.Client(
            salesforce.Config(),
            conformance.static_credentials(
                CONNECTION,
                salesforce.Credentials(
                    access_token=sdk.SecretString("salesforce-token-SENTINEL"),
                    instance_url="http://127.0.0.1:12345",
                ),
            ),
            http_client,
        )
        call = sdk.Call(
            sdk.CallID("c0a3b2d4-5e6f-5a7b-8c9d-0e1f2a3b4c5d"),
            CONNECTION,
            sdk.OperationRef("salesforce", "updateRecord"),
        )
        update_input = salesforce.UpdateRecordInput(
            "Opportunity", "006RM000001AbcdYAC", {"StageName": "Closed Won"}
        )
        update = await client.update_record().invoke(call, update_input)
        assert update.kind == "retry"
        upsert_input = salesforce.UpsertRecordByExternalIDInput(
            "Contact", "ERP_ID__c", "ERP-88213", {"LastName": "Rao"}
        )
        upsert = await client.upsert_record_by_external_id().invoke(call, upsert_input)
        assert upsert.kind == "retry"
