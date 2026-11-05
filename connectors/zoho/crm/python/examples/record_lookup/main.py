# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Serve the Zoho CRM record lookup Flow on a Dex Worker."""

import asyncio
import os

from dex import AsyncWorker, BlobCacheConfig, Registry, WorkerOptions, open_blob_cache
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.zoho_crm import new_project_connection

from .flow import RecordLookupFlow
from .models import CONNECTION_NAME


async def main() -> None:
    """Load the named connection and serve the Flow until stopped."""
    project = await load_from_environment()
    connection = None
    try:
        connection = new_project_connection(project, CONNECTION_NAME)
        cache = open_blob_cache(BlobCacheConfig(os.environ.get("DEX_BLOB_CACHE_DIRECTORY", "/tmp/dex-zoho-crm-record-cache"), 64 * 1024 * 1024))
        worker = AsyncWorker(Registry([RecordLookupFlow(connection)], allow_async_handlers=True), cache,
                             WorkerOptions(server_address=os.environ.get("DEX_SERVER_ADDRESS", "localhost:8801")))
        try:
            await worker.start()
        finally:
            await worker.close()
            cache.close()
    finally:
        if connection is not None:
            await connection.aclose()
        await project.close()


if __name__ == "__main__":
    asyncio.run(main())
