# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify Baichuan retry and persistence across a real Dex Worker and Client."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.baichuan import (
    Client,
    Config,
    Credentials,
    ListKnowledgeBasesInput,
    ListKnowledgeBasesOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, KnowledgeBaseListFlow


@pytest.mark.asyncio
async def test_knowledge_base_list_retries_503(tmp_path: Path) -> None:
    """Persist one list after the provider's first attempt returns 503."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.method == "GET"
        assert request.url.path == "/v1/kbs"
        assert request.headers["Authorization"] == "Bearer test-baichuan-key-123"
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "object": "list",
                "data": [
                    {
                        "id": "kb-abc123",
                        "object": "knowledge-base",
                        "name": "sample",
                        "description": "Example",
                        "file_ids": ["file-123"],
                        "created_at": 1701828706,
                        "updated_at": 1701828706,
                        "status": "online",
                    }
                ],
            },
        )

    reference = ConnectionRef("baichuan", CONNECTION_NAME)
    credentials = Credentials(SecretString("test-baichuan-key-123"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(base_url="http://127.0.0.1:12345"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = KnowledgeBaseListFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"baichuan-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, ListKnowledgeBasesInput())
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(ListKnowledgeBasesOutput)
                assert len(output.knowledge_bases) == 1
                assert output.knowledge_bases[0].id == "kb-abc123"
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
