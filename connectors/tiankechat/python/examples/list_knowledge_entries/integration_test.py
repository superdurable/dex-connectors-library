# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client retry test for TiankeChat listKnowledgeEntries."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.tiankechat import (
    Client,
    Config,
    Credentials,
    ListKnowledgeEntriesInput,
    ListKnowledgeEntriesOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, ListKnowledgeEntriesFlow


@pytest.mark.asyncio
async def test_list_knowledge_entries_retries_503(tmp_path: Path) -> None:
    """Exercise provider retry and result persistence through a real Dex Worker and Client."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if len(requests) == 1:
            return httpx.Response(503)
        assert request.url.path == "/api/openapi/knowledge"
        return httpx.Response(
            200,
            json={
                "ok": True,
                "total": 1,
                "page": 1,
                "limit": 20,
                "items": [{"id": 7, "category": "shipping", "active": True, "hit_count": 2}],
            },
        )

    reference = ConnectionRef("tiankechat", CONNECTION_NAME)
    credentials = Credentials("ap_REDACTED_APP_ID", SecretString("tks_REDACTED_APP_SECRET"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider_client = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider_client, reference)
        flow = ListKnowledgeEntriesFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"tiankechat-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, ListKnowledgeEntriesInput())
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=40)), 45)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(ListKnowledgeEntriesOutput)
                assert output.total == 1
                assert output.entries[0].id == 7
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
