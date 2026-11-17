# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the Bilibili Flow across a real Dex Worker and retry boundary."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.bilibili import (
    AuthorizedUser,
    Client,
    Config,
    Credentials,
    GetAuthorizedUserInput,
    new_connection,
)

from .flow import CONNECTION_NAME, AuthorizedUserFlow


@pytest.mark.asyncio
async def test_provider_query_retries_503(tmp_path: Path) -> None:
    """Persist the provider result after one availability retry."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.url.path == "/arcopen/fn/user/account/info"
        assert request.headers["Access-Token"] == "token789"
        assert request.headers["X-Bili-Accesskeyid"] == "client123"
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "code": 0,
                "data": {
                    "openid": "fc9899b46ff443cea38190d355d49f3a",
                    "name": "sample_creator",
                    "face": "https://i0.hdslb.com/bfs/face/sample.jpg",
                },
            },
        )

    reference = ConnectionRef("bilibili", CONNECTION_NAME)
    credentials = Credentials("client123", SecretString("secret456"), SecretString("token789"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(base_url="http://127.0.0.1:12345"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = AuthorizedUserFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"bilibili-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, GetAuthorizedUserInput())
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(AuthorizedUser)
                assert output.open_id == "fc9899b46ff443cea38190d355d49f3a"
                assert output.name == "sample_creator"
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
