# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-verified OpenWeatherMap requests through the existing Python package."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import httpx
import pytest

import dex_connectors.openweathermap as openweathermap

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/openweathermap/scenarios.json")
CONNECTION = sdk.ConnectionRef("openweathermap", "openweathermap")


async def invoke(scenario: dict[str, object], recorder: conformance.ExchangeRecorder,
                 _context: conformance.HeartbeatContext) -> object:
    """Map both weather operations to their recorded Go calls."""
    async with httpx.AsyncClient(transport=httpx.MockTransport(recorder.answer)) as http_client:
        client = openweathermap.Client(
            openweathermap.Config(base_url="http://127.0.0.1:21101"),
            conformance.static_credentials(
                CONNECTION, openweathermap.Credentials(sdk.SecretString("test-key-123"))),
            http_client,
        )
        operation_name = str(scenario["operation"])
        operation = (client.get_current_weather() if operation_name == "getCurrentWeather"
                     else client.get_forecast())
        data = scenario["input"]  # type: ignore[assignment]
        call = sdk.Call(sdk.CallID(str(scenario["callId"])), CONNECTION,
                        sdk.OperationRef("openweathermap", operation_name), sdk.IdempotencyKey(""))
        return await operation.invoke(call, openweathermap.WeatherInput(
            latitude=data["latitude"], longitude=data["longitude"], units=data.get("units", "")))


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match provider requests and classification against Go."""
    await conformance.replay(scenario, invoke)
