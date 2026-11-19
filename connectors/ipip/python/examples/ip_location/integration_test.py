# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the IPIP Python Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.ipip import (
    Client,
    Config,
    Credentials,
    LookupIPLocationInput,
    LookupIPLocationOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, IPLocationFlow


@pytest.mark.asyncio
async def test_ip_location_retries_503(tmp_path: Path) -> None:
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
                "ret": "ok",
                "data": {
                    "info": {
                        "country_name": "中国",
                        "region_name": "北京",
                        "city_name": "北京",
                        "country_code": "CN",
                    }
                },
            },
        )

    reference = ConnectionRef("ipip", CONNECTION_NAME)
    credentials = Credentials(SecretString("synthetic-ipip-key"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider, reference)
        flow = IPLocationFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"ipip-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, LookupIPLocationInput(ip_address="118.28.8.8"))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(LookupIPLocationOutput)
                assert output.country_code == "CN"
                assert output.city_name == "北京"
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
