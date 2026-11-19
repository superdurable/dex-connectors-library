# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the vivo Python Flow."""

import asyncio
import os
from datetime import UTC, datetime, timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.vivo import (
    Client,
    Config,
    Credentials,
    GetOnlineVersionInput,
    GetOnlineVersionOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, VivoOnlineVersionFlow


@pytest.mark.asyncio
async def test_get_online_version_retries_503(tmp_path: Path) -> None:
    """Verify a provider retry and final persisted result across a real Dex Worker."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            content=b'{"code":0,"data":{"packageName":"com.example.app","versionName":"3.2.1","versionCode":"100"}}',
        )

    reference = ConnectionRef("vivo", CONNECTION_NAME)
    credentials = Credentials(SecretString("test-account"), SecretString("test-secret"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider_client = Client(
            Config(),
            StaticCredentialProvider({reference: credentials}),
            http_client,
            lambda: datetime.fromtimestamp(1731510000, UTC),
        )
        connection = new_connection(provider_client, reference)
        flow = VivoOnlineVersionFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"vivo-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, GetOnlineVersionInput("com.example.app"))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=30)), 35)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                result_value = result.single_output(GetOnlineVersionOutput)
                assert result_value.version_code == 100
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
