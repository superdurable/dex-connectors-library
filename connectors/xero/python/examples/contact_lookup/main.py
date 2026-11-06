# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the contact lookup Worker with Dex project credentials."""

import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.xero import new_project_connection

from .flow import CONNECTION_NAME, ContactLookupFlow


async def main() -> None:
    """Load the Xero project connection and serve the registered Flow."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    cache_path = os.environ.get(
        "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-xero-python-blobs")
    )
    cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_path, 1 << 30))
    try:
        registry = dex.Registry([ContactLookupFlow(connection)], allow_async_handlers=True)
        options = dex.WorkerOptions(
            server_address=os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
        )
        async with dex.AsyncWorker(registry, cache, options) as worker:
            await worker.start()
    finally:
        cache.close()
        await connection.aclose()
        await project.close()


if __name__ == "__main__":
    asyncio.run(main())
