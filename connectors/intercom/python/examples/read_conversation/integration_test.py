# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise Intercom Query retry and completion through a real Dex Worker."""

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

from dex_connectors.intercom import (
    Client,
    Config,
    ConversationDetails,
    Credentials,
    GetConversationInput,
    new_connection,
)

from .flow import CONNECTION_NAME, ReadConversationFlow


@pytest.mark.asyncio
async def test_read_conversation_retries_in_real_dex() -> None:
    """Verify one 429 retry and a persisted conversation result."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if len(requests) == 1:
            return httpx.Response(429, headers={"Retry-After": "1"})
        return httpx.Response(
            200,
            json={
                "type": "conversation",
                "id": "2002",
                "state": "open",
                "open": True,
                "created_at": 1793008800,
                "updated_at": 1793008800,
                "source": {
                    "type": "conversation",
                    "body": "Please help",
                    "author": {"type": "user", "id": "contact-1"},
                },
                "conversation_parts": {"conversation_parts": [], "total_count": 0},
            },
        )

    reference = ConnectionRef("intercom", CONNECTION_NAME)
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_http_client:
        client = Client(
            Config(),
            StaticCredentialProvider(
                {reference: Credentials(SecretString("intercom-inert-token"), SecretString(""))}
            ),
            provider_http_client,
            endpoint="http://127.0.0.1:37345",
        )
        connection = new_connection(client, reference)
        flow = ReadConversationFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="dex-intercom-python-") as cache_directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(cache_directory) / "blobs"), 1 << 24))
            try:
                worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
                worker_task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=15)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address=server_address)
                    ) as dex_client:
                        flow_id = f"intercom-python-read-{uuid4().hex}"
                        await dex_client.start_flow(flow, flow_id, GetConversationInput("2002", 2))
                        result = await asyncio.wait_for(
                            dex_client.wait_for_flow(flow_id, timedelta(seconds=20)), 30
                        )
                        assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                        details = result.single_output(ConversationDetails)
                        assert details.conversation.id == "2002"
                        assert len(requests) == 2
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
