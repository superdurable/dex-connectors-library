# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Host the report archive Flow with the asynchronous Dex Worker."""

import os
import tempfile
from pathlib import Path

from dex import AsyncWorker, BlobCacheConfig, Flow, Registry, WorkerOptions, open_blob_cache

from .flow import ReportArchiveInput


async def run_worker(flow: Flow[ReportArchiveInput]) -> None:
    """Serve the registered report archive Flow until shutdown."""
    registry = Registry([flow], allow_async_handlers=True)
    cache_directory = os.environ.get(
        "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-s3-python-blobs")
    )
    cache = open_blob_cache(BlobCacheConfig(cache_directory, 1 << 30))
    try:
        options = WorkerOptions(server_address=os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801"))
        async with AsyncWorker(registry, cache, options) as worker:
            await worker.start()
    finally:
        cache.close()
