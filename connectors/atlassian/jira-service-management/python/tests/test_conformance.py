# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Jira Service Management provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.atlassian_jira_service_management as jsm

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402


class _JiraHeartbeatContext:
    def __init__(self, context: object) -> None:
        self._context = context
        self.attempt = context.attempt
        self.deadline = context.deadline
        self.run_id = context.run_id
        self.flow_started_at = context.flow_started_at

    def has_last_heartbeat_value(self) -> bool:
        return hasattr(self._context, "jira_marker")

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        return self._context.jira_marker

    async def heartbeat(self, marker: object) -> None:
        self._context.jira_marker = marker


async def _invoke_jira(scenario: dict[str, object], recorder: object, context: object) -> object:
    return await ADAPTER.invoke(scenario, recorder, _JiraHeartbeatContext(context))


SCENARIOS = load_scenarios(ROOT / "conformance/connectors/atlassian/jira-service-management/scenarios.json")
CONNECTION = sdk.ConnectionRef("atlassian", "jsm-test")
ADAPTER = PythonOperationAdapter(
    "jira-service-management",
    CONNECTION,
    jsm,
    lambda http_client, _: jsm.Client(
        jsm.Config(cloud_id="11223344-a1b2-4b33-8c44-def123456789", endpoint="http://localhost"),
        sdk.StaticCredentialProvider(
            {
                CONNECTION: jsm.Credentials(
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
    """Require exact request, retry, branch, output, and receipt agreement."""
    await replay(scenario, _invoke_jira)
