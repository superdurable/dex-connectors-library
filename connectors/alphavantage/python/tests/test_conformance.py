# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-verified Alpha Vantage requests through the Python package."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import httpx
import pytest

import dex_connectors.alphavantage as alphavantage

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/alphavantage/scenarios.json")
CONNECTION = sdk.ConnectionRef("alphavantage", "alphavantage")


async def invoke(scenario: dict[str, object], recorder: conformance.ExchangeRecorder,
                 _context: conformance.HeartbeatContext) -> object:
    """Map both public operations' shared StockInput to the recorded Call."""
    async with httpx.AsyncClient(transport=httpx.MockTransport(recorder.answer)) as http_client:
        client = alphavantage.Client(
            alphavantage.Config(base_url="http://127.0.0.1:21101"),
            conformance.static_credentials(
                CONNECTION, alphavantage.Credentials(sdk.SecretString("test-key-123"))),
            http_client,
        )
        operation_name = str(scenario["operation"])
        operation = (client.get_global_quote() if operation_name == "getGlobalQuote"
                     else client.get_time_series_daily())
        symbol = str(scenario["input"]["symbol"])  # type: ignore[index]
        call = sdk.Call(sdk.CallID(str(scenario["callId"])), CONNECTION,
                        sdk.OperationRef("alphavantage", operation_name), sdk.IdempotencyKey(""))
        return await operation.invoke(call, alphavantage.StockInput(symbol))


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match provider requests, outputs, and retry decisions against Go."""
    await conformance.replay(scenario, invoke)
