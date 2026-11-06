# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify the example against a real Dex Worker and Client."""

import asyncio
import json
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.microsoft_entra_id import (
    Client,
    Config,
    Credentials,
    GetUserInput,
    User,
    new_connection,
)

from .flow import CONNECTION_NAME, UserLookupFlow

USER_ID = "11111111-2222-4333-8444-555555555555"


@pytest.mark.asyncio
async def test_lookup_retries_graph_then_completes(tmp_path: Path) -> None:
    """A real Dex Worker retries a Graph 503 and routes the successful read."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[str] = []

    async def answer(reader: asyncio.StreamReader, writer: asyncio.StreamWriter) -> None:
        request = await reader.readuntil(b"\r\n\r\n")
        path = request.split(b" ", 2)[1].decode()
        requests.append(path)
        if "ffffffff" in path:
            status, body = "404 Not Found", b"{}"
        elif len(requests) == 1:
            status, body = "503 Service Unavailable", b"{}"
        else:
            status = "200 OK"
            body = json.dumps(
                {"id": USER_ID, "userPrincipalName": "ada@contoso.com", "accountEnabled": True}
            ).encode()
        headers = (
            f"HTTP/1.1 {status}\r\nContent-Type: application/json\r\n"
            f"Content-Length: {len(body)}\r\nConnection: close\r\n\r\n"
        )
        writer.write(headers.encode() + body)
        await writer.drain()
        writer.close()
        await writer.wait_closed()

    server = await asyncio.start_server(answer, "127.0.0.1", 0)
    port = server.sockets[0].getsockname()[1]
    reference = ConnectionRef("microsoft", CONNECTION_NAME)
    credentials = StaticCredentialProvider({reference: Credentials(access_token=SecretString("inert-token"))})
    client = Client(Config(), credentials, local_provider_url=f"http://127.0.0.1:{port}")
    connection = new_connection(client, reference)
    flow = UserLookupFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
    worker_task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=20)
        async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as dex_client:
            flow_id = f"entra-python-{uuid4().hex}"
            await dex_client.start_flow(flow, flow_id, GetUserInput(USER_ID))
            result = await dex_client.wait_for_flow(flow_id, timeout=timedelta(seconds=35))
            assert result.status == dex.FlowStatus.COMPLETED
            assert result.single_output(User).id == USER_ID
            missing_id = f"entra-python-missing-{uuid4().hex}"
            await dex_client.start_flow(
                flow, missing_id, GetUserInput("ffffffff-ffff-4fff-8fff-ffffffffffff")
            )
            missing = await dex_client.wait_for_flow(missing_id, timeout=timedelta(seconds=20))
            assert missing.status == dex.FlowStatus.FAILED
            assert missing.error_message is not None
            assert "notFound" in missing.error_message
            assert len(requests) == 3
    finally:
        await worker.close()
        await worker_task
        cache.close()
        await connection.aclose()
        server.close()
        await server.wait_closed()
