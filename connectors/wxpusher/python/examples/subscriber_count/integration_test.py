# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the WxPusher Python Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.wxpusher import (
    Client,
    Config,
    Credentials,
    GetSubscriberCountInput,
    SubscriberCountResult,
    new_connection,
)

from .flow import CONNECTION_NAME, WxPusherSubscriberCountFlow


@pytest.mark.asyncio
async def test_subscriber_count_retries_503(tmp_path: Path) -> None:
    """Verify a provider retry and final persisted result across a real Dex Worker."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            content=b'{"code":1000,"success":true,"data":{"total":40,"page":1,"pageSize":1,"records":[]}}',
        )

    reference = ConnectionRef("wxpusher", CONNECTION_NAME)
    credentials = Credentials(SecretString("AT_dummy"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider_client = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider_client, reference)
        flow = WxPusherSubscriberCountFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"wxpusher-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, GetSubscriberCountInput())
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=30)), 35)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                result_value = result.single_output(SubscriberCountResult)
                assert result_value.total == 40
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
