# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification of the PostHog insight list Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import pytest

from dex_connectors.posthog import InsightsPage, ListInsightsInput
from dex_connectors.posthog.posthogmock import Mock

from .flow import InsightListFlow


@pytest.mark.asyncio
async def test_insight_list_uses_default_mock_with_real_dex(tmp_path: Path) -> None:
    """Run the checked-in insight Flow through Worker, Client, and persistence."""
    mock = Mock()
    flow = InsightListFlow(mock.connection())
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
    worker_task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=15)
        async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
            flow_id = f"posthog-insights-python-{uuid4().hex}"
            await client.start_flow(flow, flow_id, ListInsightsInput(42, 0, 20))
            result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            output = result.single_output(InsightsPage)
            assert len(output.insights) == 1 and output.insights[0].id == 42
            assert len(mock.list_insights_script.calls()) == 1
    finally:
        await worker.stop()
        await worker_task
        cache.close()
