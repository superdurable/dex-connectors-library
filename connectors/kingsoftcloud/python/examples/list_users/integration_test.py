# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify kingsoftcloud Query delivery across a real Dex Worker and Client."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import pytest

from dex_connectors.kingsoftcloud import ListUsersInput, ListUsersOutput
from dex_connectors.kingsoftcloud.kingsoftcloudmock import Mock

from .flow import ListUsersFlow


@pytest.mark.asyncio
async def test_list_users_flow_with_mock(tmp_path: Path) -> None:
    """Persist the manifest mock result through a real Dex Flow."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    mock = Mock()
    flow = ListUsersFlow(mock.connection())
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
    worker_task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=15)
        async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
            flow_id = f"kingsoftcloud-python-{uuid4().hex}"
            await client.start_flow(flow, flow_id, ListUsersInput())
            result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=25)), 30)
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            assert isinstance(result.single_output(ListUsersOutput), ListUsersOutput)
    finally:
        await worker.stop()
        await worker_task
        cache.close()
        await mock.connection().aclose()
