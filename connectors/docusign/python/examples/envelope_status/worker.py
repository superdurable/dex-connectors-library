# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the DocuSign example with an asynchronous Dex Worker."""

import os
import tempfile
from pathlib import Path

import dex

from .flow import EnvelopeStatusFlow


async def run_worker(flow: EnvelopeStatusFlow) -> None:
    """Serve the registered example Flow until shutdown."""
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache_directory = os.environ.get(
        "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-docusign-python-blobs")
    )
    cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 30))
    try:
        options = dex.WorkerOptions(
            server_address=os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
        )
        async with dex.AsyncWorker(registry, cache, options) as worker:
            await worker.start()
    finally:
        cache.close()
