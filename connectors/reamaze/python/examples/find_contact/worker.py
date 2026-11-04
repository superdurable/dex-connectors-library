# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Serve the contact lookup Flow with a real asynchronous Dex Worker."""

import os
import tempfile
from pathlib import Path

import dex

from .flow import FindContactFlow


async def run_worker(flow: FindContactFlow) -> None:
    """Run the registered Flow until Worker shutdown."""
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache_path = os.environ.get(
        "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-reamaze-contact-blobs")
    )
    cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_path, 1 << 30))
    try:
        options = dex.WorkerOptions(
            server_address=os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
        )
        async with dex.AsyncWorker(registry, cache, options) as worker:
            await worker.start()
    finally:
        cache.close()
