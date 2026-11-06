# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise LinkedIn UserInfo through a real Dex Worker and Client."""

import asyncio
import os
import tempfile
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.linkedin import AuthenticatedProfile, Client, Config, Credentials, new_connection

from .flow import CONNECTION_NAME, ProfileFlow, ProfileInput


@pytest.mark.asyncio
async def test_profile_query_and_unwired_failure_in_real_dex() -> None:
    """Verify successful routing and optional failure with a local provider."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[bytes] = []

    async def answer(reader: asyncio.StreamReader, writer: asyncio.StreamWriter) -> None:
        request = await reader.readuntil(b"\r\n\r\n")
        requests.append(request)
        if len(requests) == 1:
            status, body = 200, b'{"sub":"member","email":"member@example.com","email_verified":true}'
        else:
            status, body = 404, b"{}"
        header = (
            f"HTTP/1.1 {status} {'OK' if status == 200 else 'Not Found'}\r\n"
            f"Content-Type: application/json\r\nContent-Length: {len(body)}\r\n"
            "Connection: close\r\n\r\n"
        )
        writer.write(header.encode() + body)
        await writer.drain()
        writer.close()
        await writer.wait_closed()

    provider = await asyncio.start_server(answer, "127.0.0.1", 0)
    provider_port = provider.sockets[0].getsockname()[1]
    reference = ConnectionRef("linkedin", CONNECTION_NAME)
    credentials = Credentials("", SecretString(""), SecretString("inert-test-token"), SecretString(""))
    client = Client(
        Config(user_info_url=f"http://127.0.0.1:{provider_port}/v2/userinfo"),
        StaticCredentialProvider({reference: credentials}),
    )
    connection = new_connection(client, reference)
    flow = ProfileFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    try:
        with tempfile.TemporaryDirectory(prefix="linkedin-python-dex-blobs-") as cache_directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(cache_directory) / "blobs"), 1 << 24))
            try:
                worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
                worker_task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=15)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address=server_address)
                    ) as dex_client:
                        completed_id = f"linkedin-python-{uuid4().hex}"
                        await dex_client.start_flow(flow, completed_id, ProfileInput())
                        completed = await asyncio.wait_for(
                            dex_client.wait_for_flow(completed_id, timedelta(seconds=20)), 30
                        )
                        assert completed.status == dex.FlowStatus.COMPLETED, completed.error_message
                        profile = completed.single_output(AuthenticatedProfile)
                        assert profile.subject == "member" and profile.verified_email == "member@example.com"
                        missing_id = f"linkedin-python-{uuid4().hex}"
                        await dex_client.start_flow(flow, missing_id, ProfileInput())
                        missing = await asyncio.wait_for(
                            dex_client.wait_for_flow(missing_id, timedelta(seconds=20)), 30
                        )
                        assert missing.status == dex.FlowStatus.FAILED
                        assert "notFound" in (missing.error_message or "")
                        assert len(requests) == 2
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
    finally:
        await client.aclose()
        provider.close()
        await provider.wait_closed()
