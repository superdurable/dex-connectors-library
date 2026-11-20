# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client retry test for XiaoeTech list_root_goods_categories."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.xiaoetech import (
    Client,
    Config,
    Credentials,
    ListRootGoodsCategoriesInput,
    ListRootGoodsCategoriesOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, ListRootGoodsCategoriesFlow


@pytest.mark.asyncio
async def test_list_root_goods_categories_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if len(requests) == 1:
            return httpx.Response(503)
        if request.url.path == "/token":
            return httpx.Response(
                200, text='{"code":0,"data":{"access_token":"REDACTED_ACCESS_TOKEN","expires_in":7200}}'
            )
        return httpx.Response(
            200,
            text='{"code":0,"data":[{"id":16510,"title":"Foods","parent_id":0,"has_children":1},{"id":18237,"title":"Beauty","parent_id":0,"has_children":0}]}',
        )

    reference = ConnectionRef("xiaoetech", CONNECTION_NAME)
    credentials = Credentials("app-test", "xop-test", SecretString("REDACTED_SECRET_KEY"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider_client = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider_client, reference)
        flow = ListRootGoodsCategoriesFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"xiaoetech-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, ListRootGoodsCategoriesInput())
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=40)), 45)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(ListRootGoodsCategoriesOutput)
                assert output != ListRootGoodsCategoriesOutput()
                assert len(requests) == 3
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
