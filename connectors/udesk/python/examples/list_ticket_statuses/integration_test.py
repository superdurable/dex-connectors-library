# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client retry test for Udesk listTicketStatuses."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.udesk import (
    Client,
    Config,
    Credentials,
    ListTicketStatusesInput,
    ListTicketStatusesOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, ListTicketStatusesFlow


@pytest.mark.asyncio
async def test_list_ticket_statuses_retries_503(tmp_path: Path) -> None:
    """Exercise provider retry and result persistence through a real Dex Worker and Client."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if len(requests) == 1:
            return httpx.Response(503)
        assert request.url.path == "/open_api_v1/tickets/status"
        return httpx.Response(
            200,
            json={
                "code": 1000,
                "status": [
                    {"status_id": 7, "name": "Open", "en_name": "open", "enable": True, "color": "#00aa00"}
                ],
            },
        )

    reference = ConnectionRef("udesk", CONNECTION_NAME)
    credentials = Credentials(
        SecretString("REDACTED_ADMIN@example.invalid"), SecretString("REDACTED_API_TOKEN")
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider_client = Client(
            Config(base_url="https://example.udesk.cn"),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(provider_client, reference)
        flow = ListTicketStatusesFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"udesk-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, ListTicketStatusesInput())
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=40)), 45)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(ListTicketStatusesOutput)
                assert len(output.statuses) == 1
                assert output.statuses[0].status_id == 7
                assert len(requests) == 2
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
