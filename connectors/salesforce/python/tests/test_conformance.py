# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Salesforce requests and outcomes through the Python port."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.salesforce as salesforce

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/salesforce/scenarios.json")
CONNECTION = sdk.ConnectionRef("salesforce", "salesforce-crm")
ADAPTER = conformance.PythonOperationAdapter(
    "salesforce",
    CONNECTION,
    salesforce,
    lambda http_client, _: salesforce.Client(
        salesforce.Config(),
        conformance.static_credentials(
            CONNECTION,
            salesforce.Credentials(
                access_token=sdk.SecretString("salesforce-token-SENTINEL"),
                instance_url="http://127.0.0.1:12345",
            ),
        ),
        http_client,
    ),
    {
        "queryRecords": salesforce.QueryRecordsOperation,
        "getRecord": salesforce.GetRecordOperation,
        "upsertRecordByExternalId": salesforce.UpsertRecordByExternalIDOperation,
        "updateRecord": salesforce.UpdateRecordOperation,
    },
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare requests, branches, retries, and encoded outputs to Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
