# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded OpenAI provider scenarios."""

import sys
from dataclasses import fields
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any

import dex_connectors.sdk as sdk
import httpx
import pytest

import dex_connectors.openai as openai

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import ExchangeRecorder, assert_attempt, invoke_production_mutation, load_scenarios  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/openai/scenarios.json")
CONNECTION = sdk.ConnectionRef("openai", "default")

class CheckpointContext:
    """Persist the OpenAI create checkpoint between recorded attempts."""

    def __init__(self, prior: dict[str, Any] | None) -> None:
        self.previous = openai.client.CreateResponseCheckpoint(**prior) if prior else None
        self.attempt = self.previous.attempt + 1 if self.previous else 1
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)

    def get_last_heartbeat_value(self, value_type: type[Any]) -> Any:
        return self.previous

    async def heartbeat(self, value: object = None) -> None:
        self.previous = value


def input_for(scenario: dict[str, Any]) -> openai.CreateRequest | openai.RetrieveRequest:
    """Map Go field names to the existing Python request facade."""
    input_type = openai.CreateRequest if scenario["operation"] == "createResponse" else openai.RetrieveRequest
    names = {field.metadata.get("dex_json_name", field.name): field.name for field in fields(input_type)}
    values = {names[name]: value for name, value in scenario["input"].items()}
    if "structured_output" in values:
        structured = values["structured_output"]
        values["structured_output"] = openai.StructuredOutput(
            structured["Name"],
            structured["Schema"],
            structured.get("Description", ""),
            structured.get("Strict", False),
        )
    return input_type(**values)


async def invoke(scenario: dict[str, Any], recorder: ExchangeRecorder, context: CheckpointContext) -> object:
    async with httpx.AsyncClient(transport=httpx.MockTransport(recorder.answer)) as http_client:
        client = openai.Client(
            openai.Config(endpoint="http://127.0.0.1:34788/v1"),
            sdk.StaticCredentialProvider(
                {CONNECTION: openai.Credentials(sdk.SecretString("sk-conformance"))}
            ),
            http_client,
        )
        is_create = scenario["operation"] == "createResponse"
        operation = client.create_response() if is_create else client.retrieve_response()
        call = sdk.Call(
            sdk.CallID(scenario["callId"]), CONNECTION,
            sdk.OperationRef("openai", scenario["operation"]), sdk.IdempotencyKey(""), context=context,
        )
        input_value = input_for(scenario)
        if is_create:
            return await invoke_production_mutation(operation, call, input_value, scenario["idempotencyKey"])
        return await operation.invoke(call, input_value)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare requests, retry decisions, and Dex codec outputs with Go."""
    fixture = scenario
    recorder = ExchangeRecorder(fixture, fixture["callId"], "/v1")
    context = CheckpointContext(fixture.get("priorHeartbeat"))
    for intermediate in fixture.get("intermediate", []):
        attempt = await invoke(fixture, recorder, context)
        assert_attempt(attempt, {"retry": True, "uncertain": False, "branch": "", **intermediate})
        assert recorder.request_count == intermediate["exchangeCount"]
        context.attempt += 1
    attempt = await invoke(fixture, recorder, context)
    recorder.assert_complete()
    assert_attempt(attempt, fixture["expected"])
