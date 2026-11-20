# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client retry test for Quanshi listRooms."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.quanshi import (
    Client,
    Config,
    Credentials,
    ListRoomsInput,
    ListRoomsOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, ListRoomsFlow


@pytest.mark.asyncio
async def test_list_rooms_retries_503(tmp_path: Path) -> None:
    """Exercise provider retry and result persistence through a real Dex Worker and Client."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if len(requests) == 1:
            return httpx.Response(503)
        if request.url.path == "/eventopenapi/token/v2/create":
            return httpx.Response(
                200, json={"code": 200, "data": {"token": "REDACTED_TOKEN", "userId": 88408809}}
            )
        assert request.url.path == "/confopenapi/room/list"
        return httpx.Response(
            200,
            json={
                "status": 0,
                "result": {
                    "room": [{"roomId": 10935, "roomName": "Room A", "roomCapacity": 20, "roomStatus": 1}]
                },
            },
        )

    reference = ConnectionRef("quanshi", CONNECTION_NAME)
    credentials = Credentials(SecretString("REDACTED_APP_ID"), SecretString("REDACTED_APP_SECRET"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider_client = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider_client, reference)
        flow = ListRoomsFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                flow_id = f"quanshi-python-{uuid4().hex}"
                await client.start_flow(flow, flow_id, ListRoomsInput())
                result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=40)), 45)
                assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                output = result.single_output(ListRoomsOutput)
                assert len(output.rooms) == 1
                assert output.rooms[0].id == 10935
                assert len(requests) == 3
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
