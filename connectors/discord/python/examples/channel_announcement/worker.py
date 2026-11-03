# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Serve the Discord announcement Flow with a Dex pull Worker."""

import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.discord import new_project_connection

from .flow import CONNECTION_NAME, ChannelAnnouncementFlow


async def main() -> None:
    """Load the static connection and run the Worker until shutdown."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    cache_directory = os.environ.get(
        "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-discord-python-blobs")
    )
    cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 30))
    try:
        registry = dex.Registry([ChannelAnnouncementFlow(connection)], allow_async_handlers=True)
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
