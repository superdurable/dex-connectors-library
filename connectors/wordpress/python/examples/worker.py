# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Worker entrypoint for WordPress create, lookup, and list Flows."""

import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.wordpress import new_project_connection

from .post_creation.flow import PostCreationFlow
from .post_list.flow import PostListFlow
from .post_lookup.flow import PostLookupFlow


async def run_worker() -> None:
    """Load the named Dex Web connection and register three WordPress Flows."""
    project = await load_from_environment()
    connection = None
    cache = None
    try:
        connection = new_project_connection(project, "wordpress")
        registry = dex.Registry(
            [PostCreationFlow(connection), PostLookupFlow(connection), PostListFlow(connection)],
            allow_async_handlers=True,
        )
        cache_directory = os.environ.get(
            "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-wordpress-python-blobs")
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
    """Run until the Worker stops or the process receives an interrupt."""
    asyncio.run(run_worker())


if __name__ == "__main__":
    main()
