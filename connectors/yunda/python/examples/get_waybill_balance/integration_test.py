# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client retry test for Yunda getWaybillBalance."""

import asyncio
import os
from datetime import UTC, datetime, timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.yunda import (
    Client,
    Config,
    Credentials,
    GetWaybillBalanceInput,
    GetWaybillBalanceOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, GetWaybillBalanceFlow


@pytest.mark.asyncio
async def test_get_waybill_balance_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []
    response_body = (
        '{"code":"0000","result":true,"message":"ok","data":{"status":"1","msg":"ok","free_num'
        '":"1","remain_num":"2","total_num":"0","used_num":"0"}}'
    )

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(503) if len(requests) == 1 else httpx.Response(200, text=response_body)

    reference = ConnectionRef("yunda", CONNECTION_NAME)
    credentials = Credentials(
        SecretString("999999"),
        SecretString("REDACTED_APP_SECRET"),
        SecretString("REDACTED_PARTNER_ID"),
        SecretString("REDACTED_BAIMA_SECRET"),
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider_client = Client(
            Config(),
            StaticCredentialProvider({reference: credentials}),
            http_client,
            clock=lambda: datetime.fromtimestamp(1731510000, UTC),
        )
        connection = new_connection(provider_client, reference)
        flow = GetWaybillBalanceFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"yunda-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, GetWaybillBalanceInput())
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=40)), 45)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(GetWaybillBalanceOutput)
                assert output != GetWaybillBalanceOutput()
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
