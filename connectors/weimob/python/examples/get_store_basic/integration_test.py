# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client retry test for Weimob getStoreBasic."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.weimob import (
    Client,
    Config,
    Credentials,
    GetStoreBasicInput,
    StoreBasic,
    new_connection,
)

from .flow import CONNECTION_NAME, GetStoreBasicFlow


@pytest.mark.asyncio
async def test_get_store_basic_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.url.path == "/fuwu/b/oauth2/token":
            return httpx.Response(200, json={"access_token": "safe-token", "expires_in": 3600})
        if len(requests) == 2:
            return httpx.Response(503)
        return httpx.Response(200, json={"code": {"errcode": "0"}, "data": {"bosId": 42, "bosName": "Demo"}})

    reference = ConnectionRef("weimob", CONNECTION_NAME)
    credentials = Credentials("example-client-id", SecretString("redacted-secret"), "400019")
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider_client = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider_client, reference)
        flow = GetStoreBasicFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"weimob-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, GetStoreBasicInput())
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=40)), 45)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(StoreBasic)
                assert output != StoreBasic()
                assert output.store_id == 42
                assert len(requests) == 3
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
