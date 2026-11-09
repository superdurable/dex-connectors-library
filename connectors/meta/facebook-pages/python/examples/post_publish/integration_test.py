# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for Page publishing."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from urllib.parse import parse_qs
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.meta_facebook_pages import (
    Client,
    Config,
    Credentials,
    PublishedPost,
    PublishTextPostInput,
    new_connection,
)

from .flow import CONNECTION_NAME, PostPublishFlow


@pytest.mark.asyncio
async def test_post_publish_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across real Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        assert request.headers["Authorization"] == "Bearer synthetic-page-token"
        assert request.url.path == "/123456789/feed"
        assert request.method == "POST"
        assert request.headers["Content-Type"] == "application/x-www-form-urlencoded"
        message = parse_qs(request.content.decode())["message"][0]
        requests.append(request)
        if message == "Maybe published":
            return httpx.Response(500)
        assert message == "Hello from Dex"
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(200, json={"id": "123456789_987654321"})

    reference = ConnectionRef("facebook-pages", CONNECTION_NAME)
    credentials = Credentials(SecretString("synthetic-page-token"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(base_url="http://127.0.0.1:12345"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = PostPublishFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"facebook-pages-python-publish-{uuid4().hex}"
                await client.start_flow(
                    flow, flow_id, PublishTextPostInput(page_id="123456789", message="Hello from Dex")
                )
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                assert result.single_output(PublishedPost).post_id == "123456789_987654321"
                assert len(requests) == 2
                uncertain_id = f"facebook-pages-python-uncertain-{uuid4().hex}"
                await client.start_flow(
                    flow, uncertain_id, PublishTextPostInput(page_id="123456789", message="Maybe published")
                )
                uncertain = await asyncio.wait_for(
                    client.wait_for_flow(uncertain_id, timedelta(seconds=25)), 30
                )
                assert uncertain.status == dex.FlowStatus.FAILED
                assert "uncertain" in (uncertain.error_message or "")
                assert len(requests) == 3
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
