# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify likelic Query delivery across a real Dex Worker and Client."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.likelic import (
    Client,
    Config,
    Credentials,
    ListRoomsInput,
    ListRoomsOutput,
    new_connection,
)

from .flow import ListRoomsFlow


@pytest.mark.asyncio
async def test_list_rooms_defaults_missing_total_after_retry(tmp_path: Path) -> None:
    """Persist the bounded room page after one provider retry."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    reference = ConnectionRef("likelic", "likelic")
    credentials = Credentials(customer_code="demo_company", api_key=SecretString("sk-" + "a" * 48))
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(200, json={"success": True, "data": [], "meta": {"page": 1, "limit": 20}})

    http_client = httpx.AsyncClient(transport=httpx.MockTransport(answer))
    provider = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
    connection = new_connection(provider, reference)
    flow = ListRoomsFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
    worker_task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=15)
        async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
            flow_id = f"likelic-python-{uuid4().hex}"
            await client.start_flow(flow, flow_id, ListRoomsInput(page=1))
            result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            output = result.single_output(ListRoomsOutput)
            assert output.page == 1 and output.total == 0 and not output.rooms
            assert len(requests) == 2
    finally:
        await worker.stop()
        await worker_task
        cache.close()
        await connection.aclose()
        await http_client.aclose()
