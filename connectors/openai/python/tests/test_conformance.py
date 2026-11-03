# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded OpenAI Responses through the existing Python port."""

import sys
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any

import httpx
import pytest

import dex_connectors.openai as openai
from dex_connectors import sdk
from dex_connectors.openai.client import CreateResponseCheckpoint

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/openai/scenarios.json")
CONNECTION = sdk.ConnectionRef("openai", "openai")


class OpenAIHeartbeatContext:
    """Persist the connector's own checkpoint format across fixture attempts."""

    def __init__(self) -> None:
        self.attempt = 1
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.checkpoint: CreateResponseCheckpoint | None = None

    def get_last_heartbeat_value(self, value_type: type[Any]) -> CreateResponseCheckpoint | None:
        """Return only a checkpoint written by this connector."""
        assert value_type is CreateResponseCheckpoint
        return self.checkpoint

    async def heartbeat(self, value: object) -> None:
        """Save the most recent checkpoint for the next attempt."""
        assert isinstance(value, CreateResponseCheckpoint)
        self.checkpoint = value


async def invoke_openai(scenario: dict[str, Any], recorder: conformance.ExchangeRecorder,
                        context: OpenAIHeartbeatContext) -> object:
    """Construct a real operation and use the SDK mutation path."""
    async with httpx.AsyncClient(transport=httpx.MockTransport(recorder.answer)) as http_client:
        client = openai.Client(
            openai.Config(endpoint="http://127.0.0.1:12345/v1"),
            conformance.static_credentials(
                CONNECTION, openai.Credentials(sdk.SecretString("test-secret-key"))
            ),
            http_client,
        )
        operation_name = scenario["operation"]
        request = scenario["input"]
        call = sdk.Call(sdk.CallID(scenario["callId"]), CONNECTION,
                        sdk.OperationRef("openai", operation_name), sdk.IdempotencyKey(""),
                        context=context)  # type: ignore[arg-type]
        if operation_name == "createResponse":
            structured = request.get("StructuredOutput")
            output = (openai.StructuredOutput(
                name=structured["name"], description=structured.get("description", ""),
                schema=structured["schema"], strict=structured["strict"],
            ) if structured else None)
            create_request = openai.CreateRequest(
                input=request.get("Input"), model=request.get("Model", ""),
                instructions=request.get("Instructions", ""), structured_output=output,
            )
            return await conformance.invoke_production_mutation(
                client.create_response(), call, create_request, scenario["idempotencyKey"]
            )
        return await client.retrieve_response().invoke(
            call, openai.RetrieveRequest(response_id=request["ResponseID"])
        )


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, Any]) -> None:
    """Compare provider requests, checkpoints, and outcomes to the Go recording."""
    recorder = conformance.ExchangeRecorder(scenario, scenario["callId"], "/v1")
    context = OpenAIHeartbeatContext()
    for intermediate in scenario.get("intermediate", []):
        attempt = await invoke_openai(scenario, recorder, context)
        conformance.assert_attempt(attempt, {"retry": True, "uncertain": False, "branch": "", **intermediate})
        assert recorder.request_count == intermediate["exchangeCount"]
        context.attempt += 1
    attempt = await invoke_openai(scenario, recorder, context)
    recorder.assert_complete()
    conformance.assert_attempt(attempt, scenario["expected"])
