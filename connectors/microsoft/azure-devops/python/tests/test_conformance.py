# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded AzureDevOps provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.microsoft_azure_devops as microsoft_azure_devops

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/microsoft/azure-devops/scenarios.json")
CONNECTION = sdk.ConnectionRef("azure-devops", "azure-devops")
TEST_CREDENTIALS = microsoft_azure_devops.Credentials(sdk.SecretString("test-token"))
ADAPTER = conformance.PythonOperationAdapter(
    "azure-devops",
    CONNECTION,
    microsoft_azure_devops,
    lambda http_client, recorder: microsoft_azure_devops.Client(
        microsoft_azure_devops.Config(base_url="http://127.0.0.1:8848/example"),
        conformance.static_credentials(CONNECTION, TEST_CREDENTIALS)
        if recorder.scenario["name"] != "listProjects-defect"
        else conformance.static_credentials(
            sdk.ConnectionRef("azure-devops", "missing"), TEST_CREDENTIALS
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke, path_prefix="/example")
