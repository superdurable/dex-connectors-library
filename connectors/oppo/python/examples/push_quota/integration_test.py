# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify oppo retry through a real Dex Worker and Client."""

import asyncio
import json
import os
from datetime import UTC, datetime, timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.oppo import (
    Client,
    Config,
    Credentials,
    GetPushQuotaInput,
    GetPushQuotaOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, GetPushQuotaFlow


@pytest.mark.asyncio
async def test_provider_query_retries_503(tmp_path: Path) -> None:
    """Persist the Go-recorded result after one provider availability retry."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    scenarios = json.loads(
        (Path(__file__).parents[5] / "conformance/connectors/oppo/scenarios.json").read_text()
    )["scenarios"]
    success = scenarios[0]
    exchanges = success["exchanges"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if len(requests) == 1:
            return httpx.Response(503)
        exchange = exchanges[len(requests) - 2]
        assert request.method == exchange["method"]
        assert request.url.path == exchange["path"]
        return httpx.Response(exchange["status"], content=exchange["body"])

    reference = ConnectionRef("oppo", CONNECTION_NAME)
    credentials = Credentials(SecretString("test-app-key"), SecretString("test-master-secret"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(),
            StaticCredentialProvider({reference: credentials}),
            http_client,
            clock=lambda: datetime.fromtimestamp(1731510000, UTC),
        )
        connection = new_connection(provider, reference)
        flow = GetPushQuotaFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"oppo-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, GetPushQuotaInput())
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=30)), 35)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(GetPushQuotaOutput)
                assert output.total_count == success["expected"]["output"]["totalCount"]
                assert len(requests) == len(exchanges) + 1
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
