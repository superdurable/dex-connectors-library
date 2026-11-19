# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client retry test for Newayz getCurrentWeather."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.newayz import (
    Client,
    Config,
    Credentials,
    GetCurrentWeatherInput,
    GetCurrentWeatherOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, GetCurrentWeatherFlow


@pytest.mark.asyncio
async def test_get_current_weather_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []
    response_body = (
        '{"status":"1","infocode":"10000","lives":[{"province":"湖北省","city":"武汉市"'
        ',"adcode":"420100","weather":"晴","temperature":"20","winddirection":"东风"'
        ',"windpower":"2","humidity":"50","reporttime":"2026-01-01 12:00:00"}]}'
    )

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(503) if len(requests) == 1 else httpx.Response(200, text=response_body)

    reference = ConnectionRef("newayz", CONNECTION_NAME)
    credentials = Credentials(SecretString("test-newayz-access-key"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider_client = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider_client, reference)
        flow = GetCurrentWeatherFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"newayz-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, GetCurrentWeatherInput(city_code="420100"))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=40)), 45)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(GetCurrentWeatherOutput)
                assert output != GetCurrentWeatherOutput()
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
