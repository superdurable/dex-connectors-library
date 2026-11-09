# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification of both Telegram Python Flows."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import pytest

from dex_connectors.telegram import Chat, GetChatInput, SendMessageInput, SentMessage
from dex_connectors.telegram.telegrammock import Mock

from .chat_lookup.flow import ChatLookupFlow
from .message_send.flow import MessageSendFlow


@pytest.mark.asyncio
async def test_telegram_flows_use_generated_mocks_with_real_dex(tmp_path: Path) -> None:
    """Run both checked-in Flows through Worker, Client, and Attributes."""
    mock = Mock()
    send_flow = MessageSendFlow(mock.connection())
    chat_flow = ChatLookupFlow(mock.connection())
    registry = dex.Registry([send_flow, chat_flow], allow_async_handlers=True)
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
    worker_task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=15)
        async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
            send_id = f"telegram-python-send-{uuid4().hex}"
            await client.start_flow(send_flow, send_id, SendMessageInput("@meridian_team", "Hello from Dex"))
            sent = await asyncio.wait_for(client.wait_for_flow(send_id, timedelta(seconds=25)), 30)
            assert sent.status == dex.FlowStatus.COMPLETED, sent.error_message
            message = sent.single_output(SentMessage)
            assert message.message_id == 52
            assert len(mock.send_message_script.calls()) == 1

            chat_id = f"telegram-python-chat-{uuid4().hex}"
            await client.start_flow(chat_flow, chat_id, GetChatInput("@meridian_team"))
            found = await asyncio.wait_for(client.wait_for_flow(chat_id, timedelta(seconds=25)), 30)
            assert found.status == dex.FlowStatus.COMPLETED, found.error_message
            chat = found.single_output(Chat)
            assert chat.type == "supergroup"
            assert len(mock.get_chat_script.calls()) == 1
    finally:
        await worker.stop()
        await worker_task
        cache.close()
