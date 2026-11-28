# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the Figma Python Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.figma import (
    Client,
    Config,
    Credentials,
    new_connection,
)

from .flow import CONNECTION_NAME, FigmaLookupFlow, FigmaLookupOutcome, FigmaLookupRequest


@pytest.mark.asyncio
async def test_selected_node_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.url.path.endswith("/comments"):
            return httpx.Response(200, json={"comments": [{"id": "c", "message": "Reviewed"}]})
        if not request.url.path.endswith("/nodes"):
            return httpx.Response(200, json={"name": "Design", "document": {"id": "0:1"}})
        assert request.url.params["ids"] == "0:1"
        if len(requests) == 2:
            return httpx.Response(503)
        return httpx.Response(200, json={"name": "Design", "nodes": {"0:1": {"document": {"id": "0:1"}}}})

    reference = ConnectionRef("figma", CONNECTION_NAME)
    credentials = Credentials(SecretString("fixture-key"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider, reference)
        flow = FigmaLookupFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"figma-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, FigmaLookupRequest("fileA"))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                assert result.single_output(FigmaLookupOutcome).comments[0].message == "Reviewed"
                assert [request.url.path for request in requests] == [
                    "/v1/files/fileA",
                    "/v1/files/fileA/nodes",
                    "/v1/files/fileA/nodes",
                    "/v1/files/fileA/comments",
                ]
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
