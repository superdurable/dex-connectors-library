# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Serve the Google Drive text copy Flow with a Dex Worker."""

import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.google_drive import new_project_connection

from .flow import CONNECTION_NAME, TextCopyFlow


async def run_worker() -> None:
    """Load the named connection and serve the Flow until shutdown."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    cache_directory = os.environ.get(
        "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-google-drive-python-text-copy-blobs")
    )
    cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 30))
    try:
        registry = dex.Registry([TextCopyFlow(connection)], allow_async_handlers=True)
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
