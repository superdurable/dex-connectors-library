# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one Slack channel history page through a real Dex Worker and Client."""

import asyncio
import os
import tempfile
from datetime import timedelta
from pathlib import Path
from urllib.parse import parse_qs
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.slack import (
    ChannelMessage,
    Client,
    Config,
    Credentials,
    ListChannelMessagesInput,
    ListChannelMessagesOutput,
    new_connection,
)

from .flow import CONNECTION_NAME, ChannelMessagesFlow


@pytest.mark.asyncio
async def test_channel_history_page_is_read_in_real_dex() -> None:
    """Return the page and its cursor, and send the bounded range with the bot token."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(
            200,
            json={
                "ok": True,
                "messages": [
                    {
                        "ts": "1710000002.000100",
                        "user": "U1",
                        "text": "newest",
                        "reply_count": 2,
                        "thread_ts": "1710000002.000100",
                    },
                    {"ts": "1710000001.000100", "bot_id": "B1", "text": "from the app"},
                ],
                "response_metadata": {"next_cursor": "bmV4dA=="},
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
        flow = ChannelMessagesFlow(new_connection(provider, reference))
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="slack-python-channel-blobs-") as directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 24))
            worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
            worker_task = asyncio.create_task(worker.start())
            try:
                await worker.wait_pull_registered(timeout=15)
                async with dex.AsyncClient(
                    registry, cache, dex.ClientOptions(server_address=server_address)
                ) as client:
                    flow_id = f"slack-python-channel-{uuid4().hex}"
                    request = ListChannelMessagesInput("C123", "1710000000.000100", page_size=2)
                    await client.start_flow(flow, flow_id, request)
                    completed = await asyncio.wait_for(
                        client.wait_for_flow(flow_id, timedelta(seconds=30)), 40
                    )
            finally:
                await worker.stop()
                await worker_task
                cache.close()
    assert completed.status == dex.FlowStatus.COMPLETED, completed.error_message
    assert completed.single_output(ListChannelMessagesOutput) == ListChannelMessagesOutput(
        (
            ChannelMessage("C123", "1710000002.000100", "1710000002.000100", "U1", "", "newest", 2),
            ChannelMessage("C123", "1710000001.000100", "", "", "B1", "from the app"),
        ),
        "bmV4dA==",
    )
    (sent,) = requests
    assert sent.url.path.endswith("/conversations.history")
    assert sent.headers["authorization"] == "Bearer bot-token"
    assert parse_qs(sent.url.query.decode()) == {
        "channel": ["C123"],
        "limit": ["2"],
        "oldest": ["1710000000.000100"],
    }
