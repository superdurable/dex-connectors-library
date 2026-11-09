# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded Azure Blob Storage provider scenarios."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.microsoft_azure_blob as microsoft_azure_blob

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/microsoft/azure-blob/scenarios.json")
CONNECTION = sdk.ConnectionRef("microsoft-azure-blob", "microsoft-azure-blob")
TEST_CREDENTIALS = microsoft_azure_blob.Credentials(sdk.SecretString("c2VjcmV0LXRlc3Qta2V5"))
ADAPTER = conformance.PythonOperationAdapter(
    "microsoft-azure-blob",
    CONNECTION,
    microsoft_azure_blob,
    lambda http_client, _recorder: microsoft_azure_blob.Client(
        microsoft_azure_blob.Config(endpoint="http://127.0.0.1:12345", account_name="exampleacct"),
        conformance.static_credentials(CONNECTION, TEST_CREDENTIALS),
        http_client,
        now=lambda: datetime(2026, 10, 30, tzinfo=UTC),
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke, path_prefix="")
