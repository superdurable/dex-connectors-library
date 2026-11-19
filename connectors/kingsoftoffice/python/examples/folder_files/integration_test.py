# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the WPS Flow across a real Dex Worker and retry boundary."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.kingsoftoffice import (
    Client,
    Config,
    Credentials,
    ListFolderFilesInput,
    ListFolderFilesOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, FolderFilesFlow


@pytest.mark.asyncio
async def test_provider_query_retries_503(tmp_path: Path) -> None:
    """Persist a safe folder page after one provider availability retry."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    paths: list[str] = []

    def answer(request: httpx.Request) -> httpx.Response:
        paths.append(request.url.path)
        if request.url.path == "/oauth2/token":
            assert request.method == "POST"
            assert b"client_secret=test-secret" in request.content
            return httpx.Response(
                200, json={"access_token": "sample-token", "expires_in": 7200, "token_type": "bearer"}
            )
        assert request.url.path == "/v7/drives/drive-1/files/0/children"
        assert request.headers["Authorization"] == "Bearer sample-token"
        if paths.count(request.url.path) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "code": 0,
                "data": {
                    "items": [{"id": "file-1", "name": "Notes.txt", "type": "file", "size": 12}],
                    "next_page_token": "",
                },
            },
        )

    reference = ConnectionRef("kingsoftoffice", CONNECTION_NAME)
    credentials = Credentials("AK123456", SecretString("test-secret"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(base_url="http://127.0.0.1:12345"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = FolderFilesFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"wps-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, ListFolderFilesInput("drive-1", "0"))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(ListFolderFilesOutput)
                assert output.items[0].name == "Notes.txt"
                assert len(paths) == 3
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
