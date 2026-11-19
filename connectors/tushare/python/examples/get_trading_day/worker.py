# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Worker entrypoint for the Tushare get_trading_day Flow."""

import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.tushare import new_project_connection

from .flow import CONNECTION_NAME, GetTradingDayFlow


async def run_worker() -> None:
    """Load the named Dex Web connection and serve get_trading_day Queries."""
    project = await load_from_environment()
    connection = None
    cache = None
    try:
        connection = new_project_connection(project, CONNECTION_NAME)
        registry = dex.Registry([GetTradingDayFlow(connection)], allow_async_handlers=True)
        cache_directory = os.environ.get(
            "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-tushare-get_trading_day-blobs")
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
