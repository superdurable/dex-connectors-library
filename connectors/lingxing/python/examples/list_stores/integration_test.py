# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify Lingxing Query retry and persistence across a real Dex Worker and Client."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.lingxing import (
    Client,
    Config,
    Credentials,
    ListStoresInput,
    StoreList,
    new_connection,
)

from .flow import CONNECTION_NAME, LingxingListStoresFlow


@pytest.mark.asyncio
async def test_query_retries_503_and_persists_result(tmp_path: Path) -> None:
    """Commit the typed output after an availability retry."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if len(requests) == 1:
            return httpx.Response(
                200, json={"code": 200, "data": {"access_token": "test-token", "expires_in": 7200}}
            )
        if len(requests) == 2:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "code": 0,
                "data": [{"sid": 1, "name": "Example Store", "region": "EU", "country": "DE", "status": 1}],
            },
        )

    reference = ConnectionRef("lingxing", CONNECTION_NAME)
    credentials = Credentials(SecretString("1234567890123456"), SecretString("test-secret"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider, reference)
        flow = LingxingListStoresFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(
                registry, cache, dex.ClientOptions(server_address=server_address)
            ) as client:
                flow_id = f"lingxing-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, ListStoresInput())
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                assert isinstance(result.single_output(StoreList), StoreList)
                assert len(requests) == 3
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
