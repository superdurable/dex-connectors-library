# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the Honor Python Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.honor import (
    Client,
    Config,
    Credentials,
    LookupAppIDInput,
    LookupAppIDOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, AppIDLookupFlow


@pytest.mark.asyncio
async def test_app_id_lookup_retries_503(tmp_path: Path) -> None:
    """Run token exchange and app lookup across Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []
    app_requests = 0

    def answer(request: httpx.Request) -> httpx.Response:
        nonlocal app_requests
        requests.append(request)
        if request.url.path == "/auth/token":
            return httpx.Response(
                200, json={"access_token": "test-token", "expires_in": 3600, "token_type": "Bearer"}
            )
        app_requests += 1
        if app_requests == 1:
            return httpx.Response(503)
        return httpx.Response(
            200, json={"code": 0, "data": [{"appId": 123456, "packageName": "com.example.app"}]}
        )

    reference = ConnectionRef("honor", CONNECTION_NAME)
    credentials = Credentials(SecretString("synthetic-client"), SecretString("synthetic-secret"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider, reference)
        flow = AppIDLookupFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"honor-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, LookupAppIDInput(package_name="com.example.app"))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(LookupAppIDOutput)
                assert output.app_id == 123456
                assert output.package_name == "com.example.app"
                assert len(requests) == 4
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
