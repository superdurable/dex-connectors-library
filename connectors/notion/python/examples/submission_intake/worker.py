# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start the Notion submission Flow Worker with a Dex Web connection."""

import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.notion import new_project_connection

from .flow import CONNECTION_NAME, SubmissionFlow


async def run_worker() -> None:
    """Load the named connection and serve the example Flow."""
    project = await load_from_environment()
    connection = None
    cache = None
    try:
        connection = new_project_connection(project, CONNECTION_NAME)
        registry = dex.Registry([SubmissionFlow(connection)], allow_async_handlers=True)
        directory = os.environ.get(
            "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-notion-python-blobs")
        )
        cache = dex.open_blob_cache(dex.BlobCacheConfig(directory, 1 << 30))
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
    """Serve until interrupted."""
    asyncio.run(run_worker())


if __name__ == "__main__":
    main()
