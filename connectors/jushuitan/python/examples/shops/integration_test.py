# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the Jushuitan Python Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.jushuitan import (
    Client,
    Config,
    Credentials,
    ListShopsInput,
    ListShopsOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, ShopsFlow


@pytest.mark.asyncio
async def test_shops_retries_503(tmp_path: Path) -> None:
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
                "code": 0,
                "data": {
                    "page_index": 1,
                    "page_size": 10,
                    "has_next": False,
                    "datas": [{"shop_id": 10178845, "shop_name": "Example Shop", "shop_site": "Own store"}],
                },
            },
        )

    reference = ConnectionRef("jushuitan", CONNECTION_NAME)
    credentials = Credentials(SecretString("key-123"), SecretString("secret-123"), SecretString("token-123"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider, reference)
        flow = ShopsFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"jushuitan-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, ListShopsInput(page_index=1, page_size=10))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                assert result.single_output(ListShopsOutput).shops[0].name == "Example Shop"
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
