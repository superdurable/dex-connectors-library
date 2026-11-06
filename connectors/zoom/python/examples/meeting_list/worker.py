# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Serve the Zoom meeting-list Flow using the Dex Web connection."""

import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.zoom import new_project_connection

from .flow import CONNECTION_NAME, MeetingListFlow


async def run_worker() -> None:
    """Load the named Zoom connection and serve the Flow."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    cache_directory = os.environ.get(
        "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-zoom-python-blobs")
    )
    cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 30))
    try:
        registry = dex.Registry([MeetingListFlow(connection)], allow_async_handlers=True)
        address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
        async with dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address)) as worker:
            await worker.start()
    finally:
        cache.close()
        await connection.aclose()
        await project.close()


def main() -> None:
    """Run the Worker from the Python module entrypoint."""
    asyncio.run(run_worker())


if __name__ == "__main__":
    main()
