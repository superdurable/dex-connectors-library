# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify QWeather provider retry and persistence with a real Dex Worker and Client."""

import asyncio
import json
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.qweather import (
    Client,
    Config,
    Credentials,
    GetCurrentWeatherInput,
    GetCurrentWeatherOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, GetCurrentWeatherFlow


@pytest.mark.asyncio
@pytest.mark.parametrize("condition_code", ["102", "private-test-key"], ids=["found", "credential-echo"])
async def test_query_retries_503(tmp_path: Path, condition_code: str) -> None:
    """Persist a valid observation and fail the Flow when the provider echoes the key."""
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.url.path == "/weather/v1/current/39.920000/116.410000"
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            content=json.dumps(
                {
                    "metadata": {"attributions": ["https://developer.qweather.com/attribution.html"]},
                    "validTime": "2024-05-31T03:15Z",
                    "condition": {"text": "Few Clouds", "code": condition_code},
                    "temperature": {"value": 31.71, "unit": "°C"},
                    "humidity": 0.69,
                }
            ).encode(),
        )

    reference = ConnectionRef("qweather", CONNECTION_NAME)
    credentials = Credentials(
        *(SecretString("private-test-host.qweatherapi.com"), SecretString("private-test-key"))
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider, reference)
        flow = GetCurrentWeatherFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(
            registry, cache, dex.WorkerOptions(server_address=os.environ["DEX_FLOW_SERVICE_ADDRESS"])
        )
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(
                registry, cache, dex.ClientOptions(server_address=os.environ["DEX_FLOW_SERVICE_ADDRESS"])
            ) as client:
                flow_id = f"qweather-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, GetCurrentWeatherInput(39.92, 116.41))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                if condition_code == "102":
                    assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                    output = result.single_output(GetCurrentWeatherOutput)
                    assert output.condition_code == condition_code
                else:
                    assert result.status == dex.FlowStatus.FAILED
                    assert condition_code not in (result.error_message or "")
                    assert not result.completions
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
