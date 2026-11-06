# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise Directory Query routing through a real Dex Worker and Client."""

import asyncio
import os
import tempfile
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.google_workspace_admin import (
    Client,
    Config,
    Credentials,
    ListUsersInput,
    ListUsersOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, AccountListFlow


@pytest.mark.asyncio
async def test_list_users_retries_and_unwired_rejection_fails_in_dex() -> None:
    """Verify a real Dex retry, successful page, and optional branch failure."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[str] = []

    async def provider(reader: asyncio.StreamReader, writer: asyncio.StreamWriter) -> None:
        try:
            request_line = await reader.readline()
            headers = {}
            while line := await reader.readline():
                if line == b"\r\n":
                    break
                name, _, value = line.decode().partition(":")
                headers[name.lower()] = value.strip()
            path = request_line.decode().split(" ")[1]
            requests.append(path)
            assert headers.get("authorization") == "Bearer inert-directory-token"
            if "customer=C03az79cb" in path:
                status, body, extra = "403 Forbidden", b"{}", b""
            elif len(requests) == 1:
                status, body, extra = "429 Too Many Requests", b"{}", b"Retry-After: 1\r\n"
            else:
                status = "200 OK"
                body = b'{"users":[{"id":"1001","primaryEmail":"ada@example.com"}]}'
                extra = b""
            response_head = (
                f"HTTP/1.1 {status}\r\nContent-Type: application/json\r\nContent-Length: {len(body)}\r\n"
            ).encode()
            writer.write(response_head + extra + b"Connection: close\r\n\r\n" + body)
            await writer.drain()
        finally:
            writer.close()
            await writer.wait_closed()

    server = await asyncio.start_server(provider, "127.0.0.1", 0)
    address = server.sockets[0].getsockname()
    endpoint = f"http://127.0.0.1:{address[1]}"
    reference = ConnectionRef("google", CONNECTION_NAME)
    credentials = Credentials(access_token=SecretString("inert-directory-token"))
    try:
        async with httpx.AsyncClient() as provider_client:
            connector_client = Client(
                Config(endpoint=endpoint), StaticCredentialProvider({reference: credentials}), provider_client
            )
            flow = AccountListFlow(new_connection(connector_client, reference))
            registry = dex.Registry([flow], allow_async_handlers=True)
            with tempfile.TemporaryDirectory(prefix="workspace-admin-dex-") as directory:
                cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 24))
                try:
                    worker = dex.AsyncWorker(
                        registry, cache, dex.WorkerOptions(server_address=server_address)
                    )
                    worker_task = asyncio.create_task(worker.start())
                    try:
                        await worker.wait_pull_registered(timeout=15)
                        async with dex.AsyncClient(
                            registry, cache, dex.ClientOptions(server_address=server_address)
                        ) as dex_client:
                            completed_id = f"workspace-admin-{uuid4().hex}"
                            await dex_client.start_flow(flow, completed_id, ListUsersInput())
                            completed = await asyncio.wait_for(
                                dex_client.wait_for_flow(completed_id, timedelta(seconds=20)), 30
                            )
                            assert completed.status == dex.FlowStatus.COMPLETED, completed.error_message
                            page = completed.single_output(ListUsersOutput)
                            assert page.users[0].id == "1001"
                            rejected_id = f"workspace-admin-{uuid4().hex}"
                            await dex_client.start_flow(
                                flow, rejected_id, ListUsersInput(customer="C03az79cb")
                            )
                            rejected = await asyncio.wait_for(
                                dex_client.wait_for_flow(rejected_id, timedelta(seconds=20)), 30
                            )
                            assert rejected.status == dex.FlowStatus.FAILED
                            assert "providerRejected" in (rejected.error_message or "")
                            assert len(requests) == 3
                    finally:
                        await worker.stop()
                        await worker_task
                finally:
                    cache.close()
    finally:
        server.close()
        await server.wait_closed()
