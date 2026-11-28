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
@pytest.mark.parametrize(
    "location_id", ["WX4FBXXFKE4F", "private-test-key"], ids=["fetched", "credential-echo"]
)
async def test_query_retries_503(tmp_path: Path, location_id: str) -> None:
    """Verify retry, locale settings and rejection of a private key echoed in a weather observation."""
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.url.path == "/v3/weather/now.json"
        assert request.url.params["language"] == "en"
        assert request.url.params["unit"] == "f"
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "results": [
                    {
                        "location": {"id": location_id, "name": "Beijing", "country": "CN"},
                        "now": {"text": "Clear", "code": "0", "temperature": "68"},
                        "last_update": "2026-01-01T12:00:00+08:00",
                    }
                ]
            },
        )

    reference = ConnectionRef("seniverse", CONNECTION_NAME)
    credentials = Credentials(*(SecretString("private-test-key"),))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(language="en", unit="f"), StaticCredentialProvider({reference: credentials}), http_client
        )
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
                if location_id == "WX4FBXXFKE4F":
                    assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                    output = result.single_output(GetCurrentWeatherOutput)
                    assert output.location_id == location_id
                    assert output.temperature_unit == "f"
                else:
                    assert result.status == dex.FlowStatus.FAILED
                    assert location_id not in (result.error_message or "")
                    assert not result.completions
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
