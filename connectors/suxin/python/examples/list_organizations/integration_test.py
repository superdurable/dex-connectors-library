# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification for the suxin Python Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.suxin import (
    Client,
    Config,
    Credentials,
    ListOrganizationsInput,
    ListOrganizationsOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, ListOrganizationsFlow


@pytest.mark.asyncio
async def test_list_organizations_through_dex(tmp_path: Path) -> None:
    """Run the checked-in Flow across Worker, Client, and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    responses = [
        (200, '{"success":true,"data":{"accessToken":"synthetic-token","userId":"123456"}}'),
        (
            200,
            '{"success":true,"data":[{"orgId":"1001","name":"Example Organization",'
            '"entityType":"COMPANY","certifyStatus":"VERIFIED"}]}',
        ),
    ]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        status, body = responses[len(requests) - 1]
        return httpx.Response(status, content=body.encode())

    reference = ConnectionRef("suxin", CONNECTION_NAME)
    credentials = Credentials(SecretString("app_test_123"), SecretString("sk_test_123"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider_client = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider_client, reference)
        flow = ListOrganizationsFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"suxin-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, ListOrganizationsInput())
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=40)), 45)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(ListOrganizationsOutput)
                assert len(output.organizations) == 1
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
