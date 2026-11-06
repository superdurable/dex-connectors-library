# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Graph requests and outcomes through Python operations."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.microsoft_excel as excel

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/microsoft/excel/scenarios.json")
CONNECTION = sdk.ConnectionRef("microsoft", "approvals")
ADAPTER = conformance.PythonOperationAdapter(
    "microsoft-excel",
    CONNECTION,
    excel,
    lambda http_client, recorder: excel.Client(
        excel.Config(),
        conformance.static_credentials(
            CONNECTION, excel.Credentials(access_token=sdk.SecretString("excel-token-SENTINEL"))
        ),
        http_client=http_client,
        local_provider_url="http://localhost",
        now=lambda: datetime.fromtimestamp(1, UTC),
    ),
    {
        "getTableRows": excel.GetTableRowsOperation,
        "getValues": excel.GetValuesOperation,
        "updateValues": excel.UpdateValuesOperation,
        "appendTableRows": excel.AppendTableRowsOperation,
    },
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    await conformance.replay(scenario, ADAPTER.invoke, "")
