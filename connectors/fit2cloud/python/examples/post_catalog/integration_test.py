# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify Halo article-list retry through a real Dex Worker and Client."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.fit2cloud import (
    Client,
    Config,
    Credentials,
    ListPostsInput,
    ListPostsOutput,
    PostSummary,
    new_connection,
)

from .flow import CONNECTION_NAME, PostCatalogFlow


@pytest.mark.asyncio
async def test_post_catalog_retries_503(tmp_path: Path) -> None:
    """Complete the checked-in Flow after a retry and persist its metadata."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.method == "GET"
        assert request.url.path == "/apis/content.halo.run/v1alpha1/posts"
        assert request.url.params["page"] == "2"
        assert request.url.params["size"] == "10"
        assert request.headers["Authorization"] == "Bearer pat_private-test-key"
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "items": [
                    {
                        "metadata": {"name": "example-post"},
                        "spec": {
                            "title": "Example Post",
                            "slug": "example-post",
                            "publish": True,
                            "visible": "PUBLIC",
                        },
                    }
                ],
                "hasNext": False,
                "page": 2,
                "size": 10,
                "total": 1,
            },
        )

    reference = ConnectionRef("fit2cloud", CONNECTION_NAME)
    credentials = Credentials(SecretString("pat_private-test-key"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config("https://halo.example.test"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = PostCatalogFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = "fit2cloud-python-" + uuid4().hex
                await client.start_flow(flow, flow_id, ListPostsInput(2, 10))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(ListPostsOutput)
                assert output == ListPostsOutput(
                    (PostSummary("example-post", "Example Post", "example-post", True, "PUBLIC"),), 0
                )
                assert len(requests) == 2
                assert "pat_private-test-key" not in repr(output)
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
