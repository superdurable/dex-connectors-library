# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise fofa through a real Dex Worker, retry, and persistence boundary."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.fofa import (
    Client,
    Config,
    Credentials,
    SearchDomainAssetsInput,
    SearchDomainAssetsOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, FOFADomainAssetsFlow


@pytest.mark.asyncio
async def test_domain_assets_retries_503(tmp_path: Path) -> None:
    """Persist the provider result after one availability retry."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            content=b'{"error":false,"size":9,"results":[["https://www.example.com","93.184.215.14","443"]]}',
        )

    reference = ConnectionRef("fofa", CONNECTION_NAME)
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(),
            StaticCredentialProvider({reference: Credentials(SecretString("test-secret"))}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = FOFADomainAssetsFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"fofa-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, SearchDomainAssetsInput("example.com"))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=30)), 35)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(SearchDomainAssetsOutput)
                assert output.total == 9
                assert output.assets[0].port == 443
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
