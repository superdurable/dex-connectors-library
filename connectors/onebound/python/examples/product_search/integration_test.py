# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the Onebound Python Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.onebound import (
    Client,
    Config,
    Credentials,
    ProductSearchPage,
    SearchProductsInput,
    new_connection,
)

from .flow import CONNECTION_NAME, ProductSearchPageFlow


@pytest.mark.asyncio
async def test_product_search_retries_503(tmp_path: Path) -> None:
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
                "error_code": "0000",
                "items": {
                    "page": "1",
                    "total_results": 1,
                    "item": [
                        {
                            "num_iid": "123",
                            "title": "gloves",
                            "price": "12",
                            "detail_url": "https://detail.1688.com/offer/123.html",
                        }
                    ],
                },
            },
        )

    reference = ConnectionRef("onebound", CONNECTION_NAME)
    credentials = Credentials(SecretString("testKey"), SecretString("testSecret"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider, reference)
        flow = ProductSearchPageFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"onebound-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, SearchProductsInput(keyword="gloves"))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                assert result.single_output(ProductSearchPage).total == 1
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
