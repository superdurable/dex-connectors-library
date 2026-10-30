# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the Gmail example Flow with the released async Dex Worker."""

from dex import AsyncWorker, BlobCacheConfig, Flow, Registry, WorkerOptions, open_blob_cache

from .models import ThreadReplyInput


async def run_thread_reply_worker(
    flow: Flow[ThreadReplyInput], cache_directory: str, server_address: str = "localhost:8801"
) -> None:
    """Register one Flow on a pull Worker and close its local blob cache on exit."""
    if flow is None or not cache_directory:
        raise ValueError("thread reply Flow and blob cache directory are required")
    blob_cache = open_blob_cache(BlobCacheConfig(cache_directory, 64 * 1024 * 1024))
    worker = AsyncWorker(
        Registry([flow], allow_async_handlers=True), blob_cache, WorkerOptions(server_address=server_address)
    )
    try:
        await worker.start()
    finally:
        await worker.close()
        blob_cache.close()
