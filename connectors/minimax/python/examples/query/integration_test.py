# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify Minimax Query retry and persistence across a real Dex Worker and Client."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.minimax import (
    Client,
    Config,
    Credentials,
    GetTokenPlanRemainsInput,
    GetTokenPlanRemainsOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, MinimaxGetTokenPlanRemainsFlow


@pytest.mark.asyncio
async def test_query_retries_503_and_persists_result(tmp_path: Path) -> None:
    """Commit the typed output after an availability retry."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            content='{"base_resp":{"status_code":0},"model_remains":[{"model_name":"MiniMax-M*","current_interval_remaining_percent":85,"current_weekly_remaining_percent":97,"end_time":1776373200000,"weekly_end_time":1776614400000,"current_interval_status":1}]}',
        )

    reference = ConnectionRef("minimax", CONNECTION_NAME)
    credentials = Credentials(SecretString("sk-cp-test-minimax-key"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider, reference)
        flow = MinimaxGetTokenPlanRemainsFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(
                registry, cache, dex.ClientOptions(server_address=server_address)
            ) as client:
                flow_id = f"minimax-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, GetTokenPlanRemainsInput())
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                assert isinstance(result.single_output(GetTokenPlanRemainsOutput), GetTokenPlanRemainsOutput)
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
