# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client retry test for Qince listDepartments."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.qince import (
    Client,
    Config,
    Credentials,
    ListDepartmentsInput,
    ListDepartmentsOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, ListDepartmentsFlow


@pytest.mark.asyncio
async def test_list_departments_retries_503(tmp_path: Path) -> None:
    """Run the checked-in Flow across Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []
    response_body = (
        '{"return_code":"0","msg_id":"11111111111111111111111111111111",'
        '"response_data":[{"id":"10","org_code":"DD001","org_name":"Market","org_status":"1"}]}'
    )

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(503) if len(requests) == 1 else httpx.Response(200, text=response_body)

    reference = ConnectionRef("qince", CONNECTION_NAME)
    credentials = Credentials("8519670967275282420", SecretString("2EV5i30kYI6r7Hz817"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider_client = Client(
            Config(base_url="https://openapi.region2.qince.com"),
            StaticCredentialProvider({reference: credentials}), http_client,
            clock_milliseconds=lambda: 1731510000000,
            message_id_factory=lambda: "11111111111111111111111111111111",
        )
        connection = new_connection(provider_client, reference)
        flow = ListDepartmentsFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"qince-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, ListDepartmentsInput())
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=40)), 45)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(ListDepartmentsOutput)
                assert output != ListDepartmentsOutput()
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
