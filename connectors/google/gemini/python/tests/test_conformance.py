# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Gemini requests and outcomes through the Python connector."""

import sys
from pathlib import Path
from typing import Any

import httpx
import pytest

import dex_connectors.google_gemini as gemini
from dex_connectors import sdk

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import ExchangeRecorder, HeartbeatContext, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/google/gemini/scenarios.json")
CONNECTION = sdk.ConnectionRef("google", "gemini-api")


async def invoke(scenario: dict[str, Any], recorder: ExchangeRecorder, _context: HeartbeatContext) -> object:
    """Map the Go fixture's nested content into the public Python request type."""
    request = scenario["input"]
    contents = tuple(
        gemini.Content(
            role=content["role"],
            parts=tuple(gemini.Part(text=part["text"]) for part in content["parts"]),
        )
        for content in request["contents"]
    )
    input_value = gemini.GenerateContentRequest(
        model=request["model"],
        system_instruction=request["systemInstruction"],
        contents=contents,
        response_mime_type=request["responseMimeType"],
        response_json_schema=request["responseJsonSchema"],
        temperature=request["temperature"],
        max_output_tokens=request["maxOutputTokens"],
        thinking_budget=request["thinkingBudget"],
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(recorder.answer)) as http_client:
        client = gemini.Client(
            gemini.Config(endpoint="http://127.0.0.1:22080/v1beta"),
            sdk.StaticCredentialProvider(
                {CONNECTION: gemini.Credentials(sdk.SecretString("AIzaSENTINEL-test-key_0123456789"))}
            ),
            http_client,
        )
        call = sdk.Call(
            sdk.CallID(scenario["callId"]),
            CONNECTION,
            sdk.OperationRef("gemini", "generateContent"),
            sdk.IdempotencyKey(""),
        )
        return await client.generate_content().invoke(call, input_value)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, Any]) -> None:
    """Compare complete requests and SDK-encoded outcomes with the Go recording."""
    await replay(scenario, invoke)
