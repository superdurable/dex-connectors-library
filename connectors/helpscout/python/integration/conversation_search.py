# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the Help Scout example through a real local Dex Worker and Client."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import pytest
from dex_connectors.sdk import Failure, FailureKind

from dex_connectors.helpscout import SearchConversationsInput, SearchConversationsOutput
from dex_connectors.helpscout.helpscoutmock import (
    Mock,
    search_conversations_provider_rejected_failure,
    search_conversations_retry,
    search_conversations_searched_default_answer,
)
from examples.conversation_search.flow import ConversationSearchFlow


@pytest.mark.asyncio
async def test_conversation_search_real_dex_worker(tmp_path: Path) -> None:
    """Exercise success, retry, and an unwired optional branch in Dex."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    mock = Mock()
    flow = ConversationSearchFlow(mock.connection())
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 64 * 1024 * 1024))
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
    worker_task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=30)
        async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
            success_id = f"helpscout-search-success-{uuid4()}"
            await client.start_flow(flow, success_id, SearchConversationsInput(mailbox_id=123))
            success = await client.wait_for_flow(success_id, timedelta(seconds=30))
            assert success.status == dex.FlowStatus.COMPLETED
            pages = [
                item.decode(SearchConversationsOutput)
                for item in success.completions
                if item.step_type == "CompleteConversationSearch"
            ]
            assert len(pages) == 1 and pages[0].conversations

            retry_id = f"helpscout-search-retry-{uuid4()}"
            mock.search_conversations_script.for_flow(retry_id).respond(
                search_conversations_retry(
                    Failure(FailureKind.AVAILABILITY, "helpscout", "searchConversations", "temporary outage")
                ),
                search_conversations_searched_default_answer(),
            )
            await client.start_flow(flow, retry_id, SearchConversationsInput(mailbox_id=123))
            retried = await client.wait_for_flow(retry_id, timedelta(seconds=30))
            assert retried.status == dex.FlowStatus.COMPLETED
            calls = [call for call in mock.search_conversations_script.calls() if call.flow_id == retry_id]
            assert len(calls) == 2 and calls[0].retry
            assert calls[0].call_id == calls[1].call_id

            rejected_id = f"helpscout-search-rejected-{uuid4()}"
            mock.search_conversations_script.for_flow(rejected_id).respond(
                search_conversations_provider_rejected_failure()
            )
            await client.start_flow(flow, rejected_id, SearchConversationsInput(mailbox_id=123))
            rejected = await client.wait_for_flow(rejected_id, timedelta(seconds=30))
            assert rejected.status == dex.FlowStatus.FAILED
            assert rejected.error_message is not None and "providerRejected" in rejected.error_message
            assert (
                len(
                    [call for call in mock.search_conversations_script.calls() if call.flow_id == rejected_id]
                )
                == 1
            )
        mock.search_conversations_script.assert_no_failures()
    finally:
        await worker.stop()
        await worker_task
        cache.close()
