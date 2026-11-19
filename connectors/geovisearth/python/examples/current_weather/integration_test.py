# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise geovisearth through a real Dex Worker, retry, and persistence boundary."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.geovisearth import (
    Client,
    Config,
    Credentials,
    GetCurrentWeatherInput,
    GetCurrentWeatherOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, GEOVISEarthCurrentWeatherFlow


@pytest.mark.asyncio
async def test_current_weather_retries_503(tmp_path: Path) -> None:
    """Persist the provider result after one availability retry."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            content='{"date":{"timeZone":"Asia/Shanghai","time":"20240911135031"},"result":{"wd_desc":"东北风","prs":986.7,"rh":95,"ws_desc":"3级","wp":"阴","tem":26.7,"wp_code":"02"},"location":{"path":"广西壮族自治区,崇左市,崇左","areaCode":"WTX_CH101300201"},"version":"v1","status":0}'.encode(),
        )

    reference = ConnectionRef("geovisearth", CONNECTION_NAME)
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(),
            StaticCredentialProvider({reference: Credentials(SecretString("private-test-token"))}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = GEOVISEarthCurrentWeatherFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"geovisearth-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, GetCurrentWeatherInput("WTX_CH101300201"))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=30)), 35)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(GetCurrentWeatherOutput)
                assert output.weather_code == "02"
                assert output.relative_humidity_percent == 95
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
