# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the APISpace Python Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.apispace import (
    Client,
    Config,
    Credentials,
    CurrentWeather,
    GetCurrentWeatherInput,
    new_connection,
)

from .flow import CONNECTION_NAME, CurrentWeatherFlow


@pytest.mark.asyncio
async def test_current_weather_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "status": 0,
                "result": {
                    "location": {"name": "北京"},
                    "realtime": {"text": "多云", "temp": 18.5, "rh": 38},
                    "last_update": "2026-11-13 12:00:00",
                },
            },
        )

    reference = ConnectionRef("apispace", CONNECTION_NAME)
    credentials = Credentials(SecretString("synthetic-apispace-token"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider, reference)
        flow = CurrentWeatherFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"apispace-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, GetCurrentWeatherInput(area_code="101010100"))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                assert result.single_output(CurrentWeather).city == "北京"
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
