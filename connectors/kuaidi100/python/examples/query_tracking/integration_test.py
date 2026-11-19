# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify kuaidi100 Query delivery across a real Dex Worker and Client."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import pytest

from dex_connectors.kuaidi100 import QueryTrackingInput, QueryTrackingOutput
from dex_connectors.kuaidi100.kuaidi100mock import Mock

from .flow import QueryTrackingFlow


@pytest.mark.asyncio
async def test_query_tracking_flow_with_mock(tmp_path: Path) -> None:
    """Persist the manifest mock result through a real Dex Flow."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    mock = Mock()
    flow = QueryTrackingFlow(mock.connection())
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
    worker_task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=15)
        async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
            flow_id = f"kuaidi100-python-{uuid4().hex}"
            await client.start_flow(
                flow,
                flow_id,
                QueryTrackingInput(
                    carrier_code="yuantong", tracking_number="YT1234567890123", phone="13800000000"
                ),
            )
            result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            assert isinstance(result.single_output(QueryTrackingOutput), QueryTrackingOutput)
    finally:
        await worker.stop()
        await worker_task
        cache.close()
        await mock.connection().aclose()
