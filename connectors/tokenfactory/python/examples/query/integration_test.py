# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify TokenFactory Query retry and persistence across a real Dex Worker and Client."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.tokenfactory import (
    Balance,
    Client,
    Config,
    Credentials,
    GetBalanceInput,
    new_connection,
)

from .flow import CONNECTION_NAME, TokenFactoryGetBalanceFlow


@pytest.mark.asyncio
async def test_query_retries_503_and_persists_result(tmp_path: Path) -> None:
    """Commit the typed output after an availability retry."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/gateway/v1/user/balance"
        requests.append(request)
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            content='{"balance":12.345678,"is_active":true,"unit":"CNY"}',
        )

    reference = ConnectionRef("tokenfactory", CONNECTION_NAME)
    credentials = Credentials(SecretString("sk-test-secret"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(base_url="http://127.0.0.1:12345/gateway/"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = TokenFactoryGetBalanceFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(
                registry, cache, dex.ClientOptions(server_address=server_address)
            ) as client:
                flow_id = f"tokenfactory-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, GetBalanceInput())
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                assert isinstance(result.single_output(Balance), Balance)
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
