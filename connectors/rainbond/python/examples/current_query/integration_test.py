# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify Rainbond provider retry and persistence with a real Dex Worker and Client."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.rainbond import (
    Client,
    Config,
    Credentials,
    ListTeamsInput,
    ListTeamsOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, ListTeamsFlow


@pytest.mark.asyncio
async def test_query_retries_503(tmp_path: Path) -> None:
    """Persist Rainbond output after a provider availability retry."""
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.url.path == "/openapi/v1/teams"
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            content=b'{"total":1,"tenants":[{"tenant_id":"team-1","tenant_name":"Engineering","is_active":null}]}',
        )

    reference = ConnectionRef("rainbond", CONNECTION_NAME)
    credentials = Credentials(*(SecretString("private-test-key"),))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(base_url="https://rainbond.example.com"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = ListTeamsFlow(connection)
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
                flow_id = f"rainbond-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, ListTeamsInput(1, 20))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(ListTeamsOutput)
                assert not output.teams[0].active
                assert len(output.teams) == 1
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
