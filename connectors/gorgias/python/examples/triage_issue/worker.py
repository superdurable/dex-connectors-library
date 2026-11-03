# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Worker entrypoint for the Gorgias ticket triage Flow."""

import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.gorgias import new_project_connection

from .flow import CONNECTION_NAME, TriageIssueFlow


async def run_worker() -> None:
    """Load the Dex Web connection and serve ticket triage requests."""
    project = await load_from_environment()
    connection = None
    cache = None
    try:
        connection = new_project_connection(project, CONNECTION_NAME)
        registry = dex.Registry([TriageIssueFlow(connection)], allow_async_handlers=True)
        directory = os.environ.get(
            "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-gorgias-python-blobs")
        )
        cache = dex.open_blob_cache(dex.BlobCacheConfig(directory, 1 << 30))
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
    """Run the Worker until it stops."""
    asyncio.run(run_worker())


if __name__ == "__main__":
    main()
