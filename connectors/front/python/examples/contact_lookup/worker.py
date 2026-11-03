# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Serve the Front contact lookup Flow with a Dex Worker."""

import os
import tempfile
from pathlib import Path

import dex

from .flow import ContactLookupFlow


async def run_worker(flow: ContactLookupFlow) -> None:
    """Run the Flow until the Worker is stopped."""
    registry = dex.Registry([flow], allow_async_handlers=True)
    directory = Path(
        os.environ.get("DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-front-python-blobs"))
    )
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(directory), 1 << 30))
    try:
        address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
        async with dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address)) as worker:
            await worker.start()
    finally:
        cache.close()
