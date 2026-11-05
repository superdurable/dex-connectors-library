# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start a Worker for the Typeform form catalog Flow."""

import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.typeform import new_project_connection

from .flow import CONNECTION_NAME, FormCatalogFlow


async def run_worker() -> None:
    """Load the static Typeform connection and serve form catalog requests."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    cache = dex.open_blob_cache(
        dex.BlobCacheConfig(
            os.environ.get(
                "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-typeform-catalog-blobs")
            ),
            1 << 30,
        )
    )
    try:
        registry = dex.Registry([FormCatalogFlow(connection)], allow_async_handlers=True)
        worker = dex.AsyncWorker(
            registry,
            cache,
            dex.WorkerOptions(server_address=os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")),
        )
        async with worker:
            await worker.start()
    finally:
        cache.close()
        await connection.aclose()
        await project.close()


if __name__ == "__main__":
    asyncio.run(run_worker())
