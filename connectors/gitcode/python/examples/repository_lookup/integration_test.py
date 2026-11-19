# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify GitCode repository retry through a real Dex Worker and Client."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.gitcode import (
    Client,
    Config,
    Credentials,
    GetRepositoryInput,
    GetRepositoryOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, RepositoryLookupFlow


@pytest.mark.asyncio
async def test_repository_lookup_retries_503(tmp_path: Path) -> None:
    """Complete the checked-in Flow after a retry and persist its metadata."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.method == "GET"
        assert request.url.path == "/api/v5/repos/open-source/demo"
        assert request.headers["Authorization"] == "Bearer test-gitcode-token"
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "id": 101,
                "full_name": "open-source/demo",
                "private": False,
                "default_branch": "master",
            },
        )

    reference = ConnectionRef("gitcode", CONNECTION_NAME)
    credentials = Credentials(SecretString("test-gitcode-token"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider, reference)
        flow = RepositoryLookupFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = "gitcode-python-" + uuid4().hex
                await client.start_flow(flow, flow_id, GetRepositoryInput("open-source", "demo"))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(GetRepositoryOutput)
                assert output == GetRepositoryOutput(101, "open-source/demo", False, "master")
                assert len(requests) == 2
                assert "test-gitcode-token" not in repr(output)
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
