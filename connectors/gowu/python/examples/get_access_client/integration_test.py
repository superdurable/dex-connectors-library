# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify Gowu Query retry and persistence across a real Dex Worker and Client."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.gowu import (
    AccessClientResult,
    Client,
    Config,
    Credentials,
    GetAccessClientInput,
    new_connection,
)

from .flow import CONNECTION_NAME, GowuGetAccessClientFlow


@pytest.mark.asyncio
async def test_query_retries_503_and_persists_result(tmp_path: Path) -> None:
    """Commit the typed output after an availability retry."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            content='{"appKey":"test-key","remark":"example","apiCallCount":42}',
        )

    reference = ConnectionRef("gowu", CONNECTION_NAME)
    credentials = Credentials("test-key", SecretString("test-secret"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider, reference)
        flow = GowuGetAccessClientFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(
                registry, cache, dex.ClientOptions(server_address=server_address)
            ) as client:
                flow_id = f"gowu-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, GetAccessClientInput())
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                assert isinstance(result.single_output(AccessClientResult), AccessClientResult)
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
