# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Host the Snowflake Flows with the Dex Worker."""

import os
import tempfile
from pathlib import Path

from dex import AsyncWorker, BlobCacheConfig, Registry, WorkerOptions, open_blob_cache

from dex_connectors.snowflake import Connection

from .cancel_flow import CancelStatementFlow
from .flow import StatementResultFlow


async def run_worker(connection: Connection) -> None:
    """Register both Snowflake Flows and serve them until shutdown."""
    registry = Registry(
        [StatementResultFlow(connection), CancelStatementFlow(connection)], allow_async_handlers=True
    )
    cache_directory = os.environ.get(
        "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-snowflake-python-blobs")
    )
    cache = open_blob_cache(BlobCacheConfig(cache_directory, 1 << 30))
    try:
        options = WorkerOptions(server_address=os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801"))
        async with AsyncWorker(registry, cache, options) as worker:
            await worker.start()
    finally:
        cache.close()
