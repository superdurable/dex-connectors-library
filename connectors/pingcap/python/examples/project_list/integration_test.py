# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the PingCAP Python Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.pingcap import (
    Client,
    Config,
    Credentials,
    ListProjectsInput,
    ListProjectsOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, ListProjectsOutputFlow


@pytest.mark.asyncio
async def test_project_list_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if len(requests) == 1:
            return httpx.Response(503)
        if len(requests) == 2:
            return httpx.Response(
                401,
                headers={
                    "WWW-Authenticate": (
                        'Digest realm="tidbcloud", nonce="server-nonce", qop="auth", algorithm=SHA-256'
                    )
                },
            )
        assert request.headers["Authorization"].startswith("Digest username=")
        return httpx.Response(
            200,
            json={
                "items": [
                    {"id": "101", "org_id": "201", "name": "demo", "cluster_count": 3, "user_count": 1}
                ],
                "total": 1,
            },
        )

    reference = ConnectionRef("pingcap", CONNECTION_NAME)
    credentials = Credentials(SecretString("test-public-key"), SecretString("test-private-key"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider, reference)
        flow = ListProjectsOutputFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"pingcap-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, ListProjectsInput(page=1, page_size=10))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                assert result.single_output(ListProjectsOutput).total == 1
                assert len(requests) == 3
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
