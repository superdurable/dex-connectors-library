# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise the example through a real Dex Worker and Client."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import pytest

from dex_connectors.freshworks_freshdesk import SearchTicketsInput, SearchTicketsOutput
from dex_connectors.freshworks_freshdesk.freshdeskmock import Mock

from .flow import SearchTicketsFlow


@pytest.mark.asyncio
async def test_search_tickets_real_dex_worker(tmp_path: Path) -> None:
    """Confirm the generated mock travels through Worker, Client, and Flow storage."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    mock = Mock()
    flow = SearchTicketsFlow(mock.connection())
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 64 * 1024 * 1024))
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
    worker_task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=30)
        async with dex.AsyncClient(
            registry, cache, dex.ClientOptions(server_address=server_address)
        ) as client:
            flow_id = f"freshdesk-python-search-{uuid4()}"
            await client.start_flow(flow, flow_id, SearchTicketsInput(tags=("billing-double-charge",)))
            result = await client.wait_for_flow(flow_id, timedelta(seconds=30))
            assert result.status == dex.FlowStatus.COMPLETED
            assert len(result.completions) == 1
            page = result.completions[0].decode(SearchTicketsOutput)
            assert page.total_matched == 61
    finally:
        await worker.stop()
        await worker_task
        cache.close()
