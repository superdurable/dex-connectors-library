# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the Heap Python Flow."""

import asyncio
import json
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef

from dex_connectors.heap import (
    Client,
    Config,
    SetUserPropertiesInput,
    UserPropertiesResult,
    new_connection,
)

from .flow import CONNECTION_NAME, UserPropertiesFlow


@pytest.mark.asyncio
async def test_user_properties_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across real Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.url.path == "/api/add_user_properties"
        assert json.loads(request.content) == {
            "app_id": "12345", "identity": "member-1", "properties": {"Plan": "Pro"}
        }
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(200)

    reference = ConnectionRef("heap", CONNECTION_NAME)
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(Config(app_id="12345", base_url="http://127.0.0.1:12345"), http_client)
        connection = new_connection(provider, reference)
        flow = UserPropertiesFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"heap-python-{uuid4().hex}"
                await client.start_flow(
                    flow, flow_id, SetUserPropertiesInput("member-1", {"Plan": "Pro"})
                )
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                assert result.single_output(UserPropertiesResult).accepted
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
