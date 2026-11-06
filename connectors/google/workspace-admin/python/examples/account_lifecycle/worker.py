# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the account-list Flow on a real Dex Worker."""

import os
import tempfile
from pathlib import Path

import dex

from .flow import AccountListFlow


async def run_worker(flow: AccountListFlow) -> None:
    """Register the Flow and run until the Worker stops."""
    cache_directory = os.environ.get(
        "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-workspace-admin-blobs")
    )
    cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 24))
    try:
        registry = dex.Registry([flow], allow_async_handlers=True)
        worker = dex.AsyncWorker(
            registry,
            cache,
            dex.WorkerOptions(server_address=os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")),
        )
        async with worker:
            await worker.start()
    finally:
        cache.close()
