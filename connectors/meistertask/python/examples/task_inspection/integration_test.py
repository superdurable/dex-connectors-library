# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Task and Client verification for the MeisterTask Python Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.meistertask import (
    Client,
    Config,
    Credentials,
    GetTaskInput,
    TaskSummary,
    new_connection,
)

from .flow import CONNECTION_NAME, TaskInspectionFlow


@pytest.mark.asyncio
async def test_task_inspection_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across real Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.url.path == "/tasks/15"
        assert request.headers["Authorization"] == "Bearer sample-meistertask-api-token"
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "id": 15,
                "status": 1,
                "name": "Task Name",
            },
        )

    reference = ConnectionRef("meistertask", CONNECTION_NAME)
    credentials = Credentials(SecretString("sample-meistertask-api-token"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(base_url="http://127.0.0.1:12345"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = TaskInspectionFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"meistertask-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, GetTaskInput(task_id="15"))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                assert result.single_output(TaskSummary).id == "15"
                assert result.single_output(TaskSummary).status == "1"
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
