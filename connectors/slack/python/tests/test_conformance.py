# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Slack provider outcomes through the Python SDK path."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import httpx
import pytest

import dex_connectors.slack as slack

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import ExchangeRecorder, PythonOperationAdapter, load_scenarios, replay  # noqa: E402

# listChannelMessages is Go and TypeScript only; replay just the operations this package implements.
PYTHON_OPERATIONS = {
    "listChannels", "getWorkspaceIdentity", "listThreadMessages", "getThreadReply",
    "postChannelMessage", "postThreadReply",
}
SCENARIOS = [
    scenario
    for scenario in load_scenarios(ROOT / "conformance/connectors/slack/scenarios.json")
    if scenario["operation"] in PYTHON_OPERATIONS
]
CONNECTION = sdk.ConnectionRef("slack", "workspace")


def _credentials(bot_token: str) -> slack.Credentials:
    return slack.Credentials(
        "",
        sdk.SecretString(""),
        sdk.SecretString(bot_token),
        sdk.SecretString(""),
        sdk.SecretString("user-token"),
        sdk.SecretString(""),
        sdk.SecretString(""),
    )


def _client(http_client: httpx.AsyncClient, recorder: ExchangeRecorder) -> slack.Client:
    bot_token = "" if recorder.scenario["name"] == "getWorkspaceIdentity-defect" else "bot-token"
    return slack.Client(
        slack.Config(endpoint="http://127.0.0.1:40301"),
        sdk.StaticCredentialProvider({CONNECTION: _credentials(bot_token)}),
        http_client,
    )


ADAPTER = PythonOperationAdapter("slack", CONNECTION, slack, _client, with_context=True)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match exact provider exchanges and Dex attempt decisions."""
    await replay(scenario, ADAPTER.invoke)
