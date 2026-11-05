# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Jira Service Management provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.atlassian_jira_service_management as jira_service_management

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/atlassian/jira-service-management/scenarios.json")
CONNECTION = sdk.ConnectionRef("atlassian", "jsm-test")
ADAPTER = PythonOperationAdapter(
    "jira-service-management",
    CONNECTION,
    jira_service_management,
    lambda http_client, _: jira_service_management.Client(
        jira_service_management.Config(
            cloud_id="11223344-a1b2-4b33-8c44-def123456789",
            endpoint="http://127.0.0.1",
        ),
        sdk.StaticCredentialProvider(
            {
                CONNECTION: jira_service_management.Credentials(
                    "client-id",
                    sdk.SecretString("client-secret"),
                    sdk.SecretString("jsm-access-token"),
                    sdk.SecretString("refresh-token"),
                )
            }
        ),
        http_client,
    ),
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match requests and SDK-coded outcomes against the Go recording."""
    await replay(scenario, ADAPTER.invoke)
