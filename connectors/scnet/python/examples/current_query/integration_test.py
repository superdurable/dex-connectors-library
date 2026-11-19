# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify SCNet provider retry and persistence with a real Dex Worker and Client."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.scnet import (
    Client,
    Config,
    Credentials,
    ListModelsInput,
    ListModelsOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, ListModelsFlow


@pytest.mark.asyncio
async def test_query_retries_503(tmp_path: Path) -> None:
    """Persist SCNet output after a provider availability retry."""
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.url.path == "/api/llm/v1/models"
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200, content=b'{"object":"list","data":[{"id":"example-model","object":"model"}]}'
        )

    reference = ConnectionRef("scnet", CONNECTION_NAME)
    credentials = Credentials(*(SecretString("private-test-key"),))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider, reference)
        flow = ListModelsFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(
            registry, cache, dex.WorkerOptions(server_address=os.environ["DEX_FLOW_SERVICE_ADDRESS"])
        )
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(
                registry, cache, dex.ClientOptions(server_address=os.environ["DEX_FLOW_SERVICE_ADDRESS"])
            ) as client:
                flow_id = f"scnet-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, ListModelsInput())
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(ListModelsOutput)
                assert output.model_ids == ("example-model",)
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
