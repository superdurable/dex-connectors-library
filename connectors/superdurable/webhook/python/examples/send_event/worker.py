# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Serve the signed event Flow on a Dex pull Worker."""

import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.superdurable_webhook import new_project_connection

from .flow import CONNECTION_NAME, SendEventFlow


async def run_worker() -> None:
    """Open the Dex Web connection and serve delivery requests."""
    project = await load_from_environment()
    connection = None
    try:
        connection = new_project_connection(project, CONNECTION_NAME)
        registry = dex.Registry([SendEventFlow(connection)], allow_async_handlers=True)
        directory = os.environ.get(
            "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-webhook-python-blobs")
        )
        cache = dex.open_blob_cache(dex.BlobCacheConfig(directory, 1 << 30))
        try:
            worker = dex.AsyncWorker(
                registry,
                cache,
                dex.WorkerOptions(
                    server_address=os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
                ),
            )
            async with worker:
                await worker.start()
        finally:
            cache.close()
    finally:
        if connection is not None:
            await connection.aclose()
        await project.close()


def main() -> None:
    """Run the Worker until interrupted."""
    asyncio.run(run_worker())


if __name__ == "__main__":
    main()
