# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Twilio provider exchanges through the Python SDK."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.twilio_messaging as messaging

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/twilio/messaging/scenarios.json")
CONNECTION = sdk.ConnectionRef("twilio-messaging", "twilio-messaging")
ACCOUNT = "AC" + "0" * 32
ADAPTER = PythonOperationAdapter(
    "twilio-messaging",
    CONNECTION,
    messaging,
    lambda http_client, _: messaging.Client(
        messaging.Config(
            account_sid=ACCOUNT, default_sender="+14155550100", endpoint="http://127.0.0.1:24201/2010-04-01"
        ),
        sdk.StaticCredentialProvider(
            {
                CONNECTION: messaging.Credentials(
                    auth_token=sdk.SecretString("auth-token-sentinel-0123456789ab")
                )
            }
        ),
        http_client,
    ),
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match Go's exact request, branch, retry, and codec output."""
    await replay(scenario, ADAPTER.invoke, "/2010-04-01")
