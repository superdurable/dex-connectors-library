# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the Mailchimp contact read Worker."""

import os
import tempfile
from pathlib import Path

import dex

from .flow import GetMemberFlow


async def run_worker(flow: GetMemberFlow) -> None:
    """Serve the Flow until the process stops."""
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache_directory = os.environ.get(
        "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-mailchimp-python-blobs")
    )
    cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 30))
    try:
        async with dex.AsyncWorker(
            registry,
            cache,
            dex.WorkerOptions(server_address=os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")),
        ) as worker:
            await worker.start()
    finally:
        cache.close()
