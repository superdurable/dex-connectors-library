# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the Chinaz Flow across real Dex Worker, retry, and persistence boundaries."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.chinaz import (
    Client,
    Config,
    Credentials,
    GetSiteTDKInput,
    SiteTDK,
    new_connection,
)

from .flow import CONNECTION_NAME, SiteTDKFlow


@pytest.mark.asyncio
async def test_site_tdk_retries_503(tmp_path: Path) -> None:
    """Persist the site TDK after one provider availability retry."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.url.path == "/single/tdk"
        assert request.headers["Content-Type"].startswith("multipart/form-data; boundary=")
        assert b'name="key"\r\n\r\nsecretTestKey' in request.content
        assert b'name="domain"\r\n\r\nexample.com' in request.content
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={"StateCode": 1, "Result": {
                "Title": "Example Site", "Keywords": "example",
                "Description": "Public example", "IP": "192.0.2.1",
            }},
        )

    reference = ConnectionRef("chinaz", CONNECTION_NAME)
    credentials = Credentials(SecretString("secretTestKey"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(base_url="http://127.0.0.1:12345"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = SiteTDKFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"chinaz-python-{uuid4().hex}"
                await client.start_flow(
                    flow, flow_id, GetSiteTDKInput("example.com")
                )
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(SiteTDK)
                assert output.domain == "example.com"
                assert output.title == "Example Site"
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
