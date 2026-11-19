# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client retry test for ZTO queryWaybillTrace."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.zto import (
    Client,
    Config,
    Credentials,
    QueryWaybillTraceInput,
    WaybillTrace,
    new_connection,
)

from .flow import CONNECTION_NAME, QueryWaybillTraceFlow


@pytest.mark.asyncio
async def test_query_waybill_trace_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []
    response_body = (
        '{"status":true,"statusCode":"SYS000","message":"ok","result":'
        '[{"billCode":"73100059800035","scanType":"收件","scanDate":1676429769000,'
        '"scanSite":{"name":"金华浦江县"}}]}'
    )

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(503) if len(requests) == 1 else httpx.Response(200, text=response_body)

    reference = ConnectionRef("zto", CONNECTION_NAME)
    credentials = Credentials("app123", SecretString("secretKey"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider_client = Client(
            Config(),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider_client, reference)
        flow = QueryWaybillTraceFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"zto-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, QueryWaybillTraceInput(bill_code="73100059800035"))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=40)), 45)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(WaybillTrace)
                assert output != WaybillTrace()
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
