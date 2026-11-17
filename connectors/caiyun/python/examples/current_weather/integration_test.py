# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the Caiyun example across real Dex Worker, Client, and retry persistence."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.caiyun import (
    Client,
    Config,
    Credentials,
    GetCurrentWeatherInput,
    GetCurrentWeatherOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, CurrentWeatherFlow


@pytest.mark.asyncio
async def test_current_weather_retries_503(tmp_path: Path) -> None:
    """Persist one metric observation after an unavailable provider response."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.method == "GET"
        assert request.url.path == "/v2.6/test-app-key/116.3176,39.976/realtime"
        assert request.headers["x-cy-signature"]
        assert "test-app-secret" not in str(request.url)
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "status": "ok",
                "result": {
                    "realtime": {
                        "status": "ok",
                        "temperature": 20,
                        "humidity": 0.5,
                        "skycon": "CLEAR_DAY",
                        "wind": {"speed": 2},
                    }
                },
            },
        )

    reference = ConnectionRef("caiyun", CONNECTION_NAME)
    credentials = Credentials(SecretString("test-app-key"), SecretString("test-app-secret"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(base_url="http://127.0.0.1:12345"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = CurrentWeatherFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"caiyun-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, GetCurrentWeatherInput(116.3176, 39.976))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(GetCurrentWeatherOutput)
                assert output.temperature_celsius == 20
                assert output.sky_condition == "CLEAR_DAY"
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
