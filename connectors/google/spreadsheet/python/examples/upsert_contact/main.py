# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the Google Sheets contact Worker with project OAuth configuration."""

import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.google_spreadsheet import new_project_connection

from .flow import CONNECTION_NAME, ContactFlow


async def main() -> None:
    """Load the project connection and serve the contact Flow."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    cache_directory = Path(
        os.environ.get("DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-sheets-python-blobs"))
    )
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(cache_directory), 1 << 30))
    try:
        registry = dex.Registry([ContactFlow(connection)], allow_async_handlers=True)
        address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
        async with dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address)) as worker:
            await worker.start()
    finally:
        cache.close()
        await connection.aclose()
        await project.close()


if __name__ == "__main__":
    asyncio.run(main())
