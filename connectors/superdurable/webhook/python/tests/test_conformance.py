# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded signed delivery scenarios through Python."""

import json
import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.superdurable_webhook as webhook

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/superdurable/webhook/scenarios.json")
CONNECTION = sdk.ConnectionRef("webhook", "forms")
ADAPTER = conformance.PythonOperationAdapter(
    "webhook",
    CONNECTION,
    webhook,
    lambda http_client, _: webhook.Client(
        webhook.Config(
            signature_prefix="",
            event_id_header="",
            event_id_pointer="",
            forwarded_headers=(),
            delivery_url="https://127.0.0.1:12345/hooks?tenant=forms",
        ),
        conformance.static_credentials(
            CONNECTION,
            webhook.Credentials(sdk.SecretString("whsec_c3RhbmRhcmQtd2ViaG9va3MtdGVzdC1rZXk=")),
        ),
        http_client,
        lambda: datetime(2026, 9, 30, 12, 0, 0, tzinfo=UTC),
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare exact request signatures, retry decisions, and output values."""
    async def invoke_with_go_raw_message(
        replay_scenario: dict[str, object], recorder: conformance.ExchangeRecorder,
        context: conformance.HeartbeatContext,
    ) -> object:
        input_value = replay_scenario["input"]
        assert isinstance(input_value, dict)
        payload = (json.dumps(input_value["payload"], sort_keys=True, separators=(",", ":")).encode()
                   if "payload" in input_value else b"")
        raw_scenario = {**replay_scenario, "input": {"payload": payload}}
        return await ADAPTER.invoke(raw_scenario, recorder, context)

    replay_scenario = scenario
    if scenario["name"] == "large-integer-payload":
        input_value = scenario["input"]
        assert isinstance(input_value, dict)
        exchanges = scenario["exchanges"]
        assert isinstance(exchanges, list)
        replay_scenario = {**scenario, "exchanges": [
            {**exchange, "requestBody": input_value["payload"]} for exchange in exchanges
        ]}
    await conformance.replay(replay_scenario, invoke_with_go_raw_message)
