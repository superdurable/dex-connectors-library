# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification of the PostHog event capture Flow."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import pytest

from dex_connectors.posthog import CapturedEvent
from dex_connectors.posthog.posthogmock import Mock

from .flow import EventCaptureFlow, EventCaptureRequest


@pytest.mark.asyncio
async def test_event_capture_uses_default_mock_with_real_dex(tmp_path: Path) -> None:
    """Run the checked-in Flow through Worker, Client, and Attribute persistence."""
    mock = Mock()
    flow = EventCaptureFlow(mock.connection())
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
    worker_task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=15)
        async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
            flow_id = f"posthog-python-{uuid4().hex}"
            request = EventCaptureRequest("Signed Up", "member-1", "2026-10-29T00:00:00Z")
            await client.start_flow(flow, flow_id, request)
            result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            output = result.single_output(CapturedEvent)
            assert output.accepted and output.event_id
            assert len(mock.capture_event_script.calls()) == 1
    finally:
        await worker.stop()
        await worker_task
        cache.close()
