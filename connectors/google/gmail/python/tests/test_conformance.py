# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.google_gmail as gmail

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/google/gmail/scenarios.json")
CONNECTION = sdk.ConnectionRef("google", "support")
ADAPTER = conformance.PythonOperationAdapter(
    "gmail", CONNECTION, gmail,
    lambda client, recorder: gmail.GmailProvider(
        gmail.Config(endpoint="http://127.0.0.1:12345/gmail/v1"),
        conformance.static_credentials(
            CONNECTION, gmail.Credentials(sdk.SecretString("gmail-token"), "owner@example.com")),
        conformance.NeverRefreshCredentials(recorder), client),
    {"getMessage": gmail.GetMessageOperation, "sendMessage": gmail.SendMessageOperation,
     "replyToMessage": gmail.ReplyToMessageOperation}, with_context=True,
)
@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    await conformance.replay(scenario, ADAPTER.invoke, "/gmail/v1")


def test_wrapped_prior_heartbeat_uses_json_codec() -> None:
    scenario = next(item for item in SCENARIOS if item["name"] == "sendMessage-pre-send-match")
    context = conformance.HeartbeatContext(scenario["priorHeartbeat"])
    assert context.get_last_heartbeat_value(dict[str, str | int]) == scenario["priorHeartbeat"]["value"]
