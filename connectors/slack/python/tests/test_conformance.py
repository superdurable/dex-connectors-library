# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Slack provider outcomes through the Python SDK path."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.slack as slack

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/slack/scenarios.json")
CONNECTION = sdk.ConnectionRef("slack", "workspace")
CREDENTIALS = slack.Credentials(
    "",
    sdk.SecretString(""),
    sdk.SecretString("bot-token"),
    sdk.SecretString(""),
    sdk.SecretString("user-token"),
    sdk.SecretString(""),
    sdk.SecretString(""),
)
ADAPTER = PythonOperationAdapter(
    "slack",
    CONNECTION,
    slack,
    lambda client, _: slack.Client(
        slack.Config(endpoint="http://127.0.0.1:40301"),
        sdk.StaticCredentialProvider({CONNECTION: CREDENTIALS}),
        client,
    ),
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match exact provider exchanges and Dex attempt decisions."""
    await replay(scenario, ADAPTER.invoke)
