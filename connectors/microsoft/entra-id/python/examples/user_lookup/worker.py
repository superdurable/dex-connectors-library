# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Serve the user lookup Flow on a Dex pull Worker."""

import os

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.microsoft_entra_id import new_project_connection

from .flow import CONNECTION_NAME, UserLookupFlow


async def run_worker() -> None:
    """Load the Dex Web connection and run until the Worker stops."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    cache = dex.open_blob_cache(
        dex.BlobCacheConfig(
            os.environ.get("DEX_BLOB_CACHE_DIRECTORY", "/tmp/dex-entra-user-lookup-cache"), 1 << 24
        )
    )
    worker = dex.AsyncWorker(
        dex.Registry([UserLookupFlow(connection)], allow_async_handlers=True),
        cache,
        dex.WorkerOptions(server_address=os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")),
    )
    try:
        await worker.start()
    finally:
        await worker.close()
        cache.close()
        await connection.aclose()
        await project.close()
