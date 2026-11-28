# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the Rippling Python Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.rippling import (
    Client,
    Config,
    Credentials,
    GetWorkerInput,
    WorkerPage,
    WorkerSummary,
    new_connection,
)

from .flow import CONNECTION_NAME, WorkerInspectionFlow
from .list_flow import WorkerListFlow, WorkerPageRequest


@pytest.mark.asyncio
async def test_worker_inspection_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across real Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        if request.url.path == "/workers/":
            assert request.headers["Authorization"] == "Bearer sample-rippling-api-token"
            if request.url.params.get("cursor") == "reflected":
                return httpx.Response(200, json={"results": [], "next_link": "?cursor=%73ample-rippling-api-token"})
            if "cursor" in request.url.params:
                assert dict(request.url.params) == {"cursor": "a+/="}
                return httpx.Response(200, json={"results": []})
            assert dict(request.url.params) == {"limit": "2"}
            return httpx.Response(
                200,
                json={
                    "results": [{"id": "example-2", "title": "Engineer", "status": None, "salary": 77}],
                    "next_link": "https://example.invalid/workers/?cursor=a%2B%2F%3D",
                },
            )
        requests.append(request)
        assert request.url.path == "/workers/65699281fb7bc7c3510f44a6/"
        assert request.headers["Authorization"] == "Bearer sample-rippling-api-token"
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "id": "65699281fb7bc7c3510f44a6",
                "status": "ACTIVE",
                "title": "Engineer",
            },
        )

    reference = ConnectionRef("rippling", CONNECTION_NAME)
    credentials = Credentials(SecretString("sample-rippling-api-token"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(base_url="http://127.0.0.1:12345"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = WorkerInspectionFlow(connection)
        page_flow = WorkerListFlow(connection)
        registry = dex.Registry([flow, page_flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"rippling-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, GetWorkerInput(worker_id="65699281fb7bc7c3510f44a6"))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                assert result.single_output(WorkerSummary).id == "65699281fb7bc7c3510f44a6"
                assert result.single_output(WorkerSummary).status == "ACTIVE"
                assert len(requests) == 2
                first_id = f"rippling-first-{uuid4().hex}"
                await client.start_flow(page_flow, first_id, WorkerPageRequest(limit=2))
                first = await asyncio.wait_for(client.wait_for_flow(first_id, timedelta(seconds=25)), 30)
                assert first.status == dex.FlowStatus.COMPLETED, first.error_message
                first_page = first.single_output(WorkerPage)
                assert first_page == WorkerPage((WorkerSummary("example-2", "Engineer", ""),), "a+/=")
                last_id = f"rippling-last-{uuid4().hex}"
                await client.start_flow(page_flow, last_id, WorkerPageRequest(cursor=first_page.next_cursor))
                last = await asyncio.wait_for(client.wait_for_flow(last_id, timedelta(seconds=25)), 30)
                assert last.status == dex.FlowStatus.COMPLETED, last.error_message
                assert last.single_output(WorkerPage) == WorkerPage()
                reflected_id = f"rippling-reflected-{uuid4().hex}"
                await client.start_flow(page_flow, reflected_id, WorkerPageRequest(cursor="reflected"))
                rejected = await asyncio.wait_for(client.wait_for_flow(reflected_id, timedelta(seconds=25)), 30)
                assert rejected.status == dex.FlowStatus.FAILED
                assert rejected.error_message and "defect" in rejected.error_message
                assert "sample-rippling-api-token" not in rejected.error_message
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
