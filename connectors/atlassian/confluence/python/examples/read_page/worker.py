# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Confluence read example Worker entrypoint."""

import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.atlassian_confluence import new_project_connection

from .flow import CONNECTION_NAME, ReadPageFlow


async def run_worker() -> None:
    """Serve the Flow using its named Dex Web connection."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    cache_dir = os.environ.get(
        "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-confluence-read-blobs")
    )
    cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_dir, 1 << 30))
    try:
        registry = dex.Registry([ReadPageFlow(connection)], allow_async_handlers=True)
        address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
        async with dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address)) as worker:
            await worker.start()
    finally:
        cache.close()
        await connection.aclose()
        await project.close()


def main() -> None:
    """Run the Worker until interrupted."""
    asyncio.run(run_worker())


if __name__ == "__main__":
    main()
