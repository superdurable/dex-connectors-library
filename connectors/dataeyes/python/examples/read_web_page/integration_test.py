# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify DataEyes reading across a real Dex Worker and Client retry boundary."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.dataeyes import (
    Client,
    Config,
    Credentials,
    ReadWebPageInput,
    ReadWebPageOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, ReadWebPageFlow


@pytest.mark.asyncio
async def test_read_web_page_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow through real Dex retry and persisted completion."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.method == "POST"
        assert request.url == httpx.URL("https://api.shuyanai.com/v1/reader")
        assert request.headers["Authorization"] == "Bearer test-secret"
        assert request.content == b'{"url":"https://example.com","format":"markdown"}'
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "code": 0,
                "data": {
                    "metadata": {"title": "Example Domain", "url": "private-provider-metadata"},
                    "markdown": "# Example Domain",
                    "html": "private-provider-html",
                },
                "message": "private-provider-message",
            },
        )

    reference = ConnectionRef("dataeyes", CONNECTION_NAME)
    credentials = Credentials(SecretString("test-secret"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider, reference)
        flow = ReadWebPageFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"dataeyes-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, ReadWebPageInput("https://example.com"))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(ReadWebPageOutput)
                assert output == ReadWebPageOutput("Example Domain", "# Example Domain")
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
