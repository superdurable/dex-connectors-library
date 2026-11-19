# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify Seniverse provider retry and persistence with a real Dex Worker and Client."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.seniverse import (
    Client,
    Config,
    Credentials,
    GetCurrentWeatherInput,
    GetCurrentWeatherOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, GetCurrentWeatherFlow


@pytest.mark.asyncio
async def test_query_retries_503(tmp_path: Path) -> None:
    """Persist Seniverse output after a provider availability retry."""
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.url.path == "/v3/weather/now.json"
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            content='{"results":[{"location":{"id":"WX4FBXXFKE4F","name":"北京","country":"CN"},"now":{"text":"晴","code":"0","temperature":"20"},"last_update":"2026-01-01T12:00:00+08:00"}]}'.encode(),
        )

    reference = ConnectionRef("seniverse", CONNECTION_NAME)
    credentials = Credentials(*(SecretString("private-test-key"),))
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
                flow_id = f"seniverse-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, GetCurrentWeatherInput("北京"))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(GetCurrentWeatherOutput)
                assert output.location_id == "WX4FBXXFKE4F"
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
