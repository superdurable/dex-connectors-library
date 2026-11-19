# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify Qiniu bucket retry through a real Dex Worker and Client."""

import asyncio
import os
from datetime import UTC, datetime, timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.qiniu import (
    Client,
    Config,
    Credentials,
    ListBucketsInput,
    ListBucketsOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, BucketListFlow


@pytest.mark.asyncio
async def test_bucket_list_retries_503(tmp_path: Path) -> None:
    """Complete the checked-in Flow after retrying a signed bucket request."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.method == "GET"
        assert request.url.path == "/buckets"
        assert request.headers["X-Qiniu-Date"] == "20261114T003005Z"
        assert request.headers["Authorization"].startswith("Qiniu test-access-key:")
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(200, json=["demo-images", "demo-backup"])

    reference = ConnectionRef("qiniu", CONNECTION_NAME)
    credentials = Credentials(SecretString("test-access-key"), SecretString("test-secret-key"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(),
            StaticCredentialProvider({reference: credentials}),
            http_client,
            clock=lambda: datetime(2026, 11, 14, 0, 30, 5, tzinfo=UTC),
        )
        connection = new_connection(provider, reference)
        flow = BucketListFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = "qiniu-python-" + uuid4().hex
                await client.start_flow(flow, flow_id, ListBucketsInput())
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(ListBucketsOutput)
                assert output == ListBucketsOutput(("demo-images", "demo-backup"))
                assert len(requests) == 2
                assert "test-secret-key" not in repr(output)
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
