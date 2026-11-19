# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for Hesi's Python Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.hesi import (
    Client,
    Config,
    Credentials,
    ListDepartmentsInput,
    ListDepartmentsOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, DepartmentDirectoryFlow


@pytest.mark.asyncio
async def test_department_directory_retries_503(tmp_path: Path) -> None:
    """Run token exchange, retry, and page persistence across real Dex boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.url.path.endswith("getAccessToken"):
            return httpx.Response(200, json={"value": {"accessToken": "synthetic-short-lived-token"}})
        if len([item for item in requests if item.method == "GET"]) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "count": 2,
                "items": [{"id": "corp:finance", "name": "Finance", "code": "001", "active": True}],
            },
        )

    reference = ConnectionRef("hesi", CONNECTION_NAME)
    credentials = Credentials(SecretString("synthetic-app-key"), SecretString("synthetic-app-security"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider, reference)
        flow = DepartmentDirectoryFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"hesi-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, ListDepartmentsInput(start=0, count=1))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(ListDepartmentsOutput)
                assert output.departments[0].id == "corp:finance"
                assert output.total == 2 and output.next_start == 1
                assert len([item for item in requests if item.method == "POST"]) == 2
                assert len([item for item in requests if item.method == "GET"]) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
