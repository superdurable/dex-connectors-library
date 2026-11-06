# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Serve the OAuth HTTP Flow through a Dex Worker."""

import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.superdurable_oauth2_http import new_project_connection

from .flow import CONNECTION_NAME, APIRequestFlow


async def run_worker() -> None:
    """Load the named connection and serve requests until stopped."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    cache = None
    try:
        registry = dex.Registry([APIRequestFlow(connection)], allow_async_handlers=True)
        cache_directory = os.environ.get(
            "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-oauth2-http-python-blobs")
        )
        cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 30))
        worker = dex.AsyncWorker(
            registry,
            cache,
            dex.WorkerOptions(server_address=os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")),
        )
        async with worker:
            await worker.start()
    finally:
        if cache is not None:
            cache.close()
        await connection.aclose()
        await project.close()


def main() -> None:
    """Start the Worker until interrupted."""
    asyncio.run(run_worker())


if __name__ == "__main__":
    main()
