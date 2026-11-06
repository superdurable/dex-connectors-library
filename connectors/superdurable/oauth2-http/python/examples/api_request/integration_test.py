# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise the OAuth HTTP request through a real Dex Worker and Client."""

import asyncio
import os
import tempfile
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.superdurable_oauth2_http import (
    Client,
    Config,
    Credentials,
    SendRequestInput,
    SendRequestOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, APIRequestFlow


@pytest.mark.asyncio
async def test_api_request_reaches_real_dex_worker() -> None:
    """Verify the operation, bearer header, and completed route across Dex."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, headers={"Content-Type": "application/json"}, content=b'{"sub":"user-1"}')

    reference = ConnectionRef("oauth2-http", CONNECTION_NAME)
    credentials = Credentials(
        "client", SecretString("inert-client-secret"), SecretString("inert-access-token"), SecretString("")
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_http_client:
        connection = new_connection(
            Client(
                Config(
                    authorization_endpoint="http://127.0.0.1:12345/auth",
                    token_endpoint="http://127.0.0.1:12345/token",
                    scopes=("profile",),
                    api_base_url="http://127.0.0.1:12345/v1",
                ),
                StaticCredentialProvider({reference: credentials}),
                provider_http_client,
            ),
            reference,
        )
        flow = APIRequestFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="oauth2-http-python-dex-") as directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 24))
            try:
                worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
                worker_task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=15)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address=server_address)
                    ) as client:
                        flow_id = f"oauth2-http-python-{uuid4().hex}"
                        await client.start_flow(
                            flow, flow_id, SendRequestInput(method="GET", path="/userinfo")
                        )
                        result = await asyncio.wait_for(
                            client.wait_for_flow(flow_id, timedelta(seconds=30)), 40
                        )
                        assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                        assert result.single_output(SendRequestOutput).body == '{"sub":"user-1"}'
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
                await connection.aclose()
    assert len(requests) == 1
    assert requests[0].headers["Authorization"] == "Bearer inert-access-token"
