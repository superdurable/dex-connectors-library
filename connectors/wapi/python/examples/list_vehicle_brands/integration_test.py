# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client retry test for Wapi vehicle brands."""

import asyncio
import os
from datetime import UTC, datetime, timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.wapi import (
    Client,
    Config,
    Credentials,
    ListVehicleBrandsInput,
    ListVehicleBrandsOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, ListVehicleBrandsFlow


@pytest.mark.asyncio
async def test_list_vehicle_brands_retries_503(tmp_path: Path) -> None:
    """Persist a provider result after Dex retries a temporary Wapi failure."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.url.path == "/pyi/170/324"
        assert request.url.params.get("appid") == "123"
        assert request.url.params.get("format") == "json"
        assert request.url.params.get("time") == "1795089600"
        if len(requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200,
            json={"codeid": "10000", "retdata": [{"ppid": "15", "pp_name": "宝马", "pp_price": None}]},
        )

    reference = ConnectionRef("wapi", CONNECTION_NAME)
    credentials = Credentials("123", SecretString("REDACTEDAPPSECRET000000000000000"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider_client = Client(
            Config(),
            StaticCredentialProvider({reference: credentials}),
            http_client,
            request_clock=lambda: datetime(2026, 11, 19, 12, tzinfo=UTC),
        )
        connection = new_connection(provider_client, reference)
        flow = ListVehicleBrandsFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"wapi-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, ListVehicleBrandsInput())
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=40)), 45)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(ListVehicleBrandsOutput)
                assert output.total == 1
                assert len(output.brands) == 1
                assert output.brands[0].id == "15"
                assert output.brands[0].price_fen == 0
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
