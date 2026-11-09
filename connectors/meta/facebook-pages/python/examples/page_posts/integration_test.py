# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the Facebook Pages Python Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.meta_facebook_pages import (
    Client,
    Config,
    Credentials,
    ListPostsInput,
    PagePostsPage,
    new_connection,
)

from .flow import CONNECTION_NAME, PagePostsFlow


@pytest.mark.asyncio
async def test_page_posts_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across real Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        assert request.headers["Authorization"] == "Bearer synthetic-page-token"
        assert request.url.path == "/123456789/feed"
        assert request.url.params["after"] == "cursor"
        assert request.url.params["limit"] == "100"
        requests.append(request)
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "data": [
                    {
                        "id": "123456789_987654321",
                        "message": "Welcome",
                        "created_time": "2026-11-08T10:00:00+0000",
                        "permalink_url": "https://www.facebook.com/123456789/posts/987654321",
                    }
                ],
            },
        )

    reference = ConnectionRef("facebook-pages", CONNECTION_NAME)
    credentials = Credentials(SecretString("synthetic-page-token"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(base_url="http://127.0.0.1:12345"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = PagePostsFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"facebook-pages-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, ListPostsInput(page_id="123456789", after="cursor"))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                assert result.single_output(PagePostsPage).posts[0].id == "123456789_987654321"
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
