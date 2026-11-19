# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the Caoliao Flow across real Dex Worker, retry, and persistence boundaries."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.caoliao import (
    Client,
    Config,
    Credentials,
    ListQRCodesInput,
    ListQRCodesOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, QRCodeDirectoryFlow


@pytest.mark.asyncio
async def test_qrcode_directory_retries_503(tmp_path: Path) -> None:
    """Persist the QR-code list after one provider availability retry."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.url.path == "/api/v2/rpc/qrcodes/list"
        assert request.headers["Authorization"] == "Bearer caoliao-test-secret"
        assert request.content == b'{"page_size":2,"page_token":"next-1"}'
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={"code": 0, "data": {"list": [
                {"name": "巡逻点-1", "coding": "qk3mJHJ"}
            ], "next_page_token": "next-2", "total": 3334}},
        )

    reference = ConnectionRef("caoliao", CONNECTION_NAME)
    credentials = Credentials(SecretString("caoliao-test-secret"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = QRCodeDirectoryFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"caoliao-python-{uuid4().hex}"
                await client.start_flow(
                    flow, flow_id, ListQRCodesInput(2, "next-1")
                )
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(ListQRCodesOutput)
                assert output.total == 3334
                assert output.next_page_token == "next-2"
                assert output.qr_codes[0].coding == "qk3mJHJ"
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
