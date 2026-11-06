# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Serve the Calendly scheduled event Flow using Dex Web Connections."""

import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.calendly import new_project_connection

from .flow import CONNECTION_NAME, ScheduledEventFlow


async def run_worker() -> None:
    """Load the named connection and run the Flow Worker until shutdown."""
    project = await load_from_environment()
    connection = None
    cache = None
    try:
        connection = new_project_connection(project, CONNECTION_NAME)
        registry = dex.Registry([ScheduledEventFlow(connection)], allow_async_handlers=True)
        cache_path = os.environ.get(
            "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-calendly-python-blobs")
        )
        cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_path, 1 << 30))
        async with dex.AsyncWorker(
            registry,
            cache,
            dex.WorkerOptions(server_address=os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")),
        ) as worker:
            await worker.start()
    finally:
        if connection is not None:
            await connection.aclose()
        if cache is not None:
            cache.close()
        await project.close()


def main() -> None:
    """Run the Worker from a module invocation."""
    asyncio.run(run_worker())


if __name__ == "__main__":
    main()
