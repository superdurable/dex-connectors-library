# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the tianapi Python Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.tianapi import (
    Client,
    Config,
    Credentials,
    GetHolidayStatusInput,
    GetHolidayStatusOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, GetHolidayStatusFlow


@pytest.mark.asyncio
async def test_get_holiday_status_through_dex(tmp_path: Path) -> None:
    """Run the checked-in Flow across Worker, Client, and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    responses = [(200, '{"code":200,"result":{"list":[{"date":"2021-01-01","daycode":1,"isnotwork":1}]}}')]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        status, body = responses[len(requests) - 1]
        return httpx.Response(status, content=body.encode())

    reference = ConnectionRef("tianapi", CONNECTION_NAME)
    credentials = Credentials(SecretString("test-secret"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider_client = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider_client, reference)
        flow = GetHolidayStatusFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"tianapi-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, GetHolidayStatusInput(date="2021-01-01"))
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=40)), 45)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(GetHolidayStatusOutput)
                assert output.date == "2021-01-01"
                assert len(requests) == 1
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
