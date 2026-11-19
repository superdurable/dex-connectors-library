# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the JD Logistics Flow across a real Dex Worker and retry boundary."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.jdlogistics import (
    Client,
    Config,
    Credentials,
    QueryWaybillTraceInput,
    WaybillTrace,
    new_connection,
)

from .flow import CONNECTION_NAME, WaybillTraceFlow


@pytest.mark.asyncio
async def test_provider_query_retries_503(tmp_path: Path) -> None:
    """Persist the provider result after one availability retry."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.url.path == "/ecap/v1/orders/trace/query"
        assert request.url.params["app_key"] == "app123"
        assert request.url.params["access_token"] == "token123"
        assert request.url.params["sign"]
        assert b"JDX009866825381" in request.content
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={
                "success": True,
                "code": 0,
                "data": {
                    "traceDetails": [
                        {
                            "waybillCode": "JDX009866825381",
                            "state": "200001",
                            "categoryName": "已揽收",
                            "operationTime": "2022-04-07 20:22:50",
                            "operatorPhone": "discard-me",
                        }
                    ]
                },
            },
        )

    reference = ConnectionRef("jdlogistics", CONNECTION_NAME)
    credentials = Credentials("app123", SecretString("secretKey"), SecretString("token123"), "010K123")
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(
            Config(base_url="http://127.0.0.1:12345"),
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
                flow_id = f"jdlogistics-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, QueryWaybillTraceInput("JDX009866825381"))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(WaybillTrace)
                assert output.waybill_no == "JDX009866825381"
                assert len(output.events) == 1
                assert output.events[0].state == "200001"
                assert "discard-me" not in repr(output)
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
