# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client retry test for Useasy listAgents."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.useasy import (
    Client,
    Config,
    Credentials,
    ListAgentsInput,
    ListAgentsOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, ListAgentsFlow


@pytest.mark.asyncio
async def test_list_agents_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []
    response_body = (
        '{"success":true,"code":"200","data":{"list":[{"_id":"663578842498871236",'
        '"agentNumber":"8010","status":1}],"page":{"pageSize":10,"pageNumber":2,'
        '"totalPage":3,"total":21}}}'
    )

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(503) if len(requests) == 1 else httpx.Response(200, text=response_body)

    reference = ConnectionRef("useasy", CONNECTION_NAME)
    credentials = Credentials(SecretString("1011"), SecretString("private-test-key"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider_client = Client(
            Config(), StaticCredentialProvider({reference: credentials}), http_client,
            clock_seconds=lambda: 1731510000, nonce_factory=lambda: "123456",
        )
        connection = new_connection(provider_client, reference)
        flow = ListAgentsFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"useasy-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, ListAgentsInput(page=2, page_size=10))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=40)), 45)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(ListAgentsOutput)
                assert output != ListAgentsOutput()
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
