# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded WeCom requests and fenced Mutation outcomes."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.tencent_wecom as wecom

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/tencent/wecom/scenarios.json")
ROBOT = sdk.ConnectionRef("wecom", "wecom-robot")
APPLICATION = sdk.ConnectionRef("wecom", "wecom-application")
CLIENTS: dict[conformance.ExchangeRecorder, wecom.Client] = {}


def create_client(http_client: object, recorder: conformance.ExchangeRecorder) -> wecom.Client:
    """Bind synthetic credentials for both WeCom auth methods."""
    import httpx

    assert isinstance(http_client, httpx.AsyncClient)
    credentials = sdk.StaticCredentialProvider(
        {
            ROBOT: wecom.Credentials(
                auth_method_id="group-robot", webhook_key=sdk.SecretString("REDACTED-WEBHOOK-KEY")
            ),
            APPLICATION: wecom.Credentials(
                auth_method_id="application",
                corp_id="ww1a2b3c4d5e6f7a8b",
                agent_id="1000002",
                corp_secret=sdk.SecretString("REDACTED_CORP_SECRET"),
            ),
        }
    )
    client = wecom.Client(wecom.Config(), credentials, http_client)
    if recorder in CLIENTS:
        client._token_slots = CLIENTS[recorder]._token_slots
    CLIENTS[recorder] = client
    return client


ADAPTERS = {
    "sendGroupRobotMessage": conformance.PythonOperationAdapter(
        "wecom",
        ROBOT,
        wecom,
        create_client,
        with_context=True,
    ),
    "sendApplicationMessage": conformance.PythonOperationAdapter(
        "wecom",
        APPLICATION,
        wecom,
        create_client,
        with_context=True,
    ),
}


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match Go requests, outputs, retry decisions, and Fence heartbeats."""
    await conformance.replay(scenario, ADAPTERS[scenario["operation"]].invoke)
