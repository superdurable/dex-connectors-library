# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Serve the Hiver conversation claim Flow with a Dex Worker."""

import os
import tempfile
from pathlib import Path

import dex

from .flow import ClaimConversationFlow


async def run_worker(flow: ClaimConversationFlow) -> None:
    """Run the checked-in Flow until the Worker stops."""
    registry = dex.Registry([flow], allow_async_handlers=True)
    directory = Path(
        os.environ.get("DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-hiver-claim-blobs"))
    )
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(directory), 1 << 30))
    try:
        address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
        async with dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address)) as worker:
            await worker.start()
    finally:
        cache.close()
