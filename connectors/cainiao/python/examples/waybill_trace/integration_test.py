# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the Cainiao example through a real Dex Worker, Client, and retry boundary."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from urllib.parse import parse_qs
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.cainiao import (
    Client,
    Config,
    Credentials,
    QueryWaybillTraceInput,
    WaybillTrace,
    new_connection,
)

from .flow import CONNECTION_NAME, WaybillTraceFlow


@pytest.mark.asyncio
async def test_waybill_trace_retries_503(tmp_path: Path) -> None:
    """Persist the event history after a retryable provider response."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.method == "POST"
        assert request.url.path == "/router/rest"
        form = parse_qs(request.content.decode())
        assert form["cp_code"] == ["YTO"]
        assert form["waybill_code"] == ["12345"]
        assert form["sign"] and "secretKey" not in request.content.decode()
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "cainiao_waybill_logisticstrace_get_response": {
                    "logistics_detail_dto_list": {
                        "logistics_detail_dto": [
                            {"action": "GOT", "description": "已揽收", "time": "2016-07-14 17:00:42"}
                        ]
                    }
                }
            },
        )

    reference = ConnectionRef("cainiao", CONNECTION_NAME)
    credentials = Credentials("123456", SecretString("secretKey"), SecretString("sessionToken"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(base_url="http://127.0.0.1:12345/router/rest"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider, reference)
        flow = WaybillTraceFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"cainiao-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, QueryWaybillTraceInput("YTO", "12345"))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(WaybillTrace)
                assert output.waybill_code == "12345"
                assert output.events[0].action == "GOT"
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
