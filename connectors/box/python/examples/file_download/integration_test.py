# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise Box download through a real Dex Worker and Client."""

import asyncio
import os
import tempfile
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import pytest

from dex_connectors.box import DownloadedFile, DownloadFileInput
from dex_connectors.box.boxmock import Mock

from .flow import DownloadFileFlow


@pytest.mark.asyncio
async def test_download_mock_through_real_dex() -> None:
    """Verify Worker routing, typed output, and Attribute persistence."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    mock = Mock()
    flow = DownloadFileFlow(mock.connection())
    registry = dex.Registry([flow], allow_async_handlers=True)
    with tempfile.TemporaryDirectory(prefix="box-python-dex-") as directory:
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 24))
        try:
            worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
            worker_task = asyncio.create_task(worker.start())
            try:
                await worker.wait_pull_registered(timeout=15)
                async with dex.AsyncClient(
                    registry, cache, dex.ClientOptions(server_address=address)
                ) as client:
                    flow_id = f"box-python-download-{uuid4().hex}"
                    await client.start_flow(flow, flow_id, DownloadFileInput("12345"))
                    result = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=20)), 30)
                    assert result.status == dex.FlowStatus.COMPLETED, result.error_message
                    assert result.single_output(DownloadedFile) == DownloadedFile("12345", "SGVsbG8=", 5)
            finally:
                await worker.stop()
                await worker_task
        finally:
            cache.close()
