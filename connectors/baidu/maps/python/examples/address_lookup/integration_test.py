# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the Baidu Maps Flow across real Dex Worker, retry, and persistence boundaries."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.baidu_maps import (
    Client,
    Config,
    Credentials,
    GeocodeAddressInput,
    GeocodeAddressOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, BaiduAddressLookupFlow


@pytest.mark.asyncio
async def test_address_lookup_retries_503(tmp_path: Path) -> None:
    """Persist the geocoding result after one provider availability retry."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.url.path == "/geocoding/v3/"
        assert request.url.params["ak"] == "test-baidu-server-ak-123"
        assert request.url.params["address"] == "北京市海淀区上地十街10号"
        assert request.url.params["city"] == "北京市"
        assert request.url.params["output"] == "json"
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "status": 0,
                "result": {
                    "location": {"lng": 116.30783584945, "lat": 40.056583335558},
                    "precise": 1,
                    "confidence": 80,
                    "comprehension": 100,
                    "level": "门址",
                },
            },
        )

    reference = ConnectionRef("baidu-maps", CONNECTION_NAME)
    credentials = Credentials(SecretString("test-baidu-server-ak-123"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(base_url="http://127.0.0.1:12345"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = BaiduAddressLookupFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"baidu-maps-python-{uuid4().hex}"
                await client.start_flow(
                    flow, flow_id, GeocodeAddressInput("北京市海淀区上地十街10号", "北京市")
                )
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(GeocodeAddressOutput)
                assert output.longitude == 116.30783584945
                assert output.latitude == 40.056583335558
                assert output.precise
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
