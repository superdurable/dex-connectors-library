# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise Slack post and thread read through a real Dex Worker and Client."""

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

from dex_connectors.slack import Client, Config, Credentials, ListThreadMessagesOutput, new_connection

from .flow import CONNECTION_NAME, ThreadMessageInput, ThreadMessagesFlow


@pytest.mark.asyncio
async def test_post_and_thread_read_in_real_dex() -> None:
    """Confirm a post, subsequent Query, and unwired rejection through Dex."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.url.path.endswith("chat.postMessage"):
            if b"reject" in request.content:
                return httpx.Response(403, json={"ok": False, "error": "no_permission"})
            return httpx.Response(
                200,
                json={
                    "ok": True,
                    "channel": "C123",
                    "ts": "1710000000.000100",
                    "message": {"ts": "1710000000.000100", "text": "Hello from Dex"},
                },
            )
        return httpx.Response(
            200,
            json={
                "ok": True,
                "messages": [{"ts": "1710000000.000100", "text": "Hello from Dex"}],
            },
        )

    reference = ConnectionRef("slack", CONNECTION_NAME)
    credentials = Credentials(
        "",
        SecretString(""),
        SecretString("bot-token"),
        SecretString(""),
        SecretString("user-token"),
        SecretString(""),
        SecretString(""),
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as provider_http_client:
        provider = Client(
            Config(endpoint="http://127.0.0.1:40301"),
            StaticCredentialProvider({reference: credentials}),
            provider_http_client,
        )
        flow = ThreadMessagesFlow(new_connection(provider, reference))
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="slack-python-dex-blobs-") as directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 24))
            try:
                worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
                worker_task = asyncio.create_task(worker.start())
                try:
                    await worker.wait_pull_registered(timeout=15)
                    async with dex.AsyncClient(
                        registry, cache, dex.ClientOptions(server_address=server_address)
                    ) as dex_client:
                        flow_id = f"slack-python-{uuid4().hex}"
                        await dex_client.start_flow(
                            flow, flow_id, ThreadMessageInput("C123", "Hello from Dex")
                        )
                        completed = await asyncio.wait_for(
                            dex_client.wait_for_flow(flow_id, timedelta(seconds=30)), 40
                        )
                        assert completed.status == dex.FlowStatus.COMPLETED, completed.error_message
                        page = completed.single_output(ListThreadMessagesOutput)
                        assert len(page.messages) == 1 and page.messages[0].text == "Hello from Dex"
                        rejected_id = f"slack-python-{uuid4().hex}"
                        await dex_client.start_flow(flow, rejected_id, ThreadMessageInput("C123", "reject"))
                        rejected = await asyncio.wait_for(
                            dex_client.wait_for_flow(rejected_id, timedelta(seconds=30)), 40
                        )
                        assert rejected.status == dex.FlowStatus.FAILED
                        assert "providerRejected" in (rejected.error_message or "")
                        assert len(requests) == 3
                finally:
                    await worker.stop()
                    await worker_task
            finally:
                cache.close()
