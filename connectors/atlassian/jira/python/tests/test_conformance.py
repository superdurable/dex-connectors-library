# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Jira provider exchanges through Python Dex operations."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.atlassian_jira as jira

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/atlassian/jira/scenarios.json")
CONNECTION = sdk.ConnectionRef("atlassian", "jira-test")
ADAPTER = PythonOperationAdapter(
    "jira",
    CONNECTION,
    jira,
    lambda http_client, _: jira.Client(
        jira.Config(cloud_id="11223344-a1b2-4b33-8c44-def123456789", endpoint="http://127.0.0.1:12345"),
        sdk.StaticCredentialProvider(
            {
                CONNECTION: jira.Credentials(
                    "client-id",
                    sdk.SecretString("client-secret"),
                    sdk.SecretString("jira-access-token"),
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
    """Compare requests, retries, branches, and exact SDK-encoded outputs."""
    await replay(scenario, ADAPTER.invoke, "/ex/jira/11223344-a1b2-4b33-8c44-def123456789/rest/api/3")
