# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run all five Notion Steps through a real local Dex Worker and Client."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import pytest
from dex_connectors.sdk import Failure, FailureKind

from dex_connectors.notion import GetPageOutput
from dex_connectors.notion.notionmock import (
    Mock,
    search_no_shared_title,
    search_retry,
    search_submissions_search,
)

from .flow import CONNECTION_NAME, SubmissionFlow, SubmissionRequest


@pytest.mark.asyncio
async def test_submission_flow_on_real_dex(tmp_path: Path) -> None:
    """Verify the full route, retry reuse, and an unwired optional branch."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    mock = Mock(CONNECTION_NAME)
    flow = SubmissionFlow(mock.connection())
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 25))
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
    task = asyncio.create_task(worker.start())
    request = SubmissionRequest(
        "2a4d6f80-1b3c-4d5e-8f70-112233445566",
        "2a4d6f80-1b3c-4d5e-8f70-112233445577",
        "Ada Lovelace",
        "ada@example.com",
    )
    try:
        await worker.wait_pull_registered(timeout=30)
        async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
            success_id = f"notion-python-success-{uuid4().hex}"
            await client.start_flow(flow, success_id, request)
            success = await client.wait_for_flow(success_id, timedelta(seconds=30))
            assert success.status == dex.FlowStatus.COMPLETED, success.error_message
            output = success.single_output(GetPageOutput)
            assert output.page.title == "Ada Lovelace"
            assert len([call for call in mock.search_script.calls() if call.flow_id == success_id]) == 1
            assert (
                len([call for call in mock.query_database_script.calls() if call.flow_id == success_id]) == 1
            )
            assert len([call for call in mock.create_page_script.calls() if call.flow_id == success_id]) == 1
            assert (
                len(
                    [
                        call
                        for call in mock.update_page_properties_script.calls()
                        if call.flow_id == success_id
                    ]
                )
                == 1
            )
            assert len([call for call in mock.get_page_script.calls() if call.flow_id == success_id]) == 1

            retry_id = f"notion-python-retry-{uuid4().hex}"
            mock.search_script.for_flow(retry_id).respond(
                search_retry(Failure(FailureKind.AVAILABILITY, "notion", "search", "temporary outage")),
                search_submissions_search(),
            )
            await client.start_flow(flow, retry_id, request)
            retried = await client.wait_for_flow(retry_id, timedelta(seconds=30))
            assert retried.status == dex.FlowStatus.COMPLETED, retried.error_message
            calls = [call for call in mock.search_script.calls() if call.flow_id == retry_id]
            assert len(calls) == 2 and calls[0].call_id == calls[1].call_id

            rejected_id = f"notion-python-unwired-{uuid4().hex}"
            mock.search_script.for_flow(rejected_id).respond(search_no_shared_title())
            await client.start_flow(flow, rejected_id, request)
            rejected = await client.wait_for_flow(rejected_id, timedelta(seconds=30))
            assert rejected.status == dex.FlowStatus.FAILED
            assert rejected.error_message is not None and "noMatch" in rejected.error_message
    finally:
        await worker.stop()
        await task
        cache.close()
