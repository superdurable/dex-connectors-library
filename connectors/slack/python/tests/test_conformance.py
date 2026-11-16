# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Slack provider outcomes through the Python SDK path."""

import dataclasses
import sys
from pathlib import Path
from typing import Any

import dex_connectors.sdk as sdk
import httpx
import pytest

import dex_connectors.slack as slack

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import ExchangeRecorder, PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/slack/scenarios.json")
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


class _FixtureButtonMessageOperation:
    """Decode the fixture's JSON buttons, which the generic adapter passes through as dicts."""

    def __init__(self, client: slack.Client) -> None:
        self.operation = client.post_button_message()

    def definition(self) -> sdk.MutationDefinition:
        return self.operation.definition()

    def idempotency_key(self, call_id: sdk.CallID, input: slack.PostButtonMessageInput) -> sdk.IdempotencyKey:
        return self.operation.idempotency_key(call_id, input)

    async def invoke(self, call: sdk.Call, input: slack.PostButtonMessageInput) -> Any:
        buttons = tuple(
            slack.MessageButton(
                button["actionId"], button["text"], button.get("value", ""), button.get("style")
            )
            for button in input.buttons
        )
        return await self.operation.invoke(call, dataclasses.replace(input, buttons=buttons))


ADAPTER = PythonOperationAdapter(
    "slack",
    CONNECTION,
    slack,
    _client,
    {"postButtonMessage": _FixtureButtonMessageOperation},
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match exact provider exchanges and Dex attempt decisions."""
    await replay(scenario, ADAPTER.invoke)
