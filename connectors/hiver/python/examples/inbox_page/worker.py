# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the Hiver inbox page Flow in a Dex Worker."""

import os
import tempfile
from pathlib import Path

import dex

from .flow import InboxPageFlow


async def run_worker(flow: InboxPageFlow) -> None:
    """Serve the checked-in Flow until the Worker is stopped."""
    registry = dex.Registry([flow], allow_async_handlers=True)
    directory = Path(
        os.environ.get("DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-hiver-python-blobs"))
    )
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(directory), 1 << 30))
    try:
        address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
        async with dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address)) as worker:
            await worker.start()
    finally:
        cache.close()
