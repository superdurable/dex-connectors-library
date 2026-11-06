# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise getFile through a real Dex Worker and Client."""

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

from dex_connectors.microsoft_onedrive import Client, Config, Credentials, DriveItem, new_connection

from .flow import CONNECTION_NAME, GetFileFlow, GetFileRequest


@pytest.mark.asyncio
async def test_get_file_retries_and_fails_unwired_rejection_in_real_dex() -> None:
    """Cover Worker registration, retry, success, and an optional failure route."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.url.path.endswith("/rejected"):
            return httpx.Response(403, json={"error": {"code": "accessDenied"}})
        if len(requests) == 1:
            return httpx.Response(429, headers={"Retry-After": "1"})
        return httpx.Response(
            200, json={"id": "file-1", "name": "report.txt", "size": 6, "file": {"mimeType": "text/plain"}}
        )

    reference = ConnectionRef("microsoft", CONNECTION_NAME)
    credentials = StaticCredentialProvider(
        {reference: Credentials(access_token=SecretString("inert-graph-token"))}
    )
    with tempfile.TemporaryDirectory(prefix="onedrive-python-dex-") as directory:
        async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
            connection = new_connection(
                Client(Config(endpoint="http://127.0.0.1:40692"), credentials, http_client), reference
            )
            flow = GetFileFlow(connection)
            registry = dex.Registry([flow], allow_async_handlers=True)
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 24))
            try:
                worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
                worker_task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=15)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address=address)
                    ) as client:
                        successful_flow_id = f"onedrive-python-get-{uuid4().hex}"
                        await client.start_flow(
                            flow, successful_flow_id, GetFileRequest("b!teamDriveKey-2", "file-1")
                        )
                        success = await asyncio.wait_for(
                            client.wait_for_flow(successful_flow_id, timedelta(seconds=20)), 30
                        )
                        assert success.status == dex.FlowStatus.COMPLETED, success.error_message
                        assert success.single_output(DriveItem).id == "file-1"
                        assert len(requests) == 2

                        rejected_flow_id = f"onedrive-python-get-{uuid4().hex}"
                        await client.start_flow(
                            flow, rejected_flow_id, GetFileRequest("b!teamDriveKey-2", "rejected")
                        )
                        rejected = await asyncio.wait_for(
                            client.wait_for_flow(rejected_flow_id, timedelta(seconds=20)), 30
                        )
                        assert rejected.status == dex.FlowStatus.FAILED
                        assert "providerRejected" in (rejected.error_message or "")
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
                await connection.aclose()
