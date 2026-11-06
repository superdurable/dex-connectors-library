# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the Zoho CRM record lookup Worker with Dex Web credentials."""

import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.zoho_crm import new_project_connection

from .flow import CONNECTION_NAME, RecordLookupFlow


async def run_worker() -> None:
    """Load the static connection and serve read-only record lookups."""
    project = await load_from_environment()
    connection = None
    cache = None
    try:
        connection = new_project_connection(project, CONNECTION_NAME)
        flow = RecordLookupFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache_dir = os.environ.get(
            "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-zoho-crm-record-blobs")
        )
        cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_dir, 1 << 30))
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
    """Start the example Worker until interrupted."""
    asyncio.run(run_worker())


if __name__ == "__main__":
    main()
