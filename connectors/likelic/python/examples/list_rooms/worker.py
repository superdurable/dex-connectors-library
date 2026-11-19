# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Worker entrypoint for the likelic Query example."""

import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.likelic import new_project_connection

from .flow import CONNECTION_NAME, ListRoomsFlow


async def run_worker() -> None:
    """Load the named connection and serve likelic Flow requests."""
    project = await load_from_environment()
    connection = None
    cache = None
    try:
        connection = new_project_connection(project, CONNECTION_NAME)
        registry = dex.Registry([ListRoomsFlow(connection)], allow_async_handlers=True)
        cache_directory = os.environ.get(
            "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-likelic-list_rooms-blobs")
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
        if connection is not None:
            await connection.aclose()
        if cache is not None:
            cache.close()
        await project.close()


def main() -> None:
    """Serve until the Worker stops or receives an interrupt."""
    asyncio.run(run_worker())


if __name__ == "__main__":
    main()
