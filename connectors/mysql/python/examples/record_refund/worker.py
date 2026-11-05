# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Serve the MySQL refund Flow with a project connection."""

import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.mysql import new_project_connection

from .flow import CONNECTION_NAME, RecordRefundFlow


async def run_worker() -> None:
    """Load the mysql-ledger connection and run the Dex Worker."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    cache = dex.open_blob_cache(
        dex.BlobCacheConfig(str(Path(tempfile.gettempdir()) / "dex-mysql-python-refund-blobs"), 1 << 30)
    )
    try:
        registry = dex.Registry([RecordRefundFlow(connection)], allow_async_handlers=True)
        worker = dex.AsyncWorker(
            registry,
            cache,
            dex.WorkerOptions(server_address=os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")),
        )
        async with worker:
            await worker.start()
    finally:
        await connection.aclose()
        cache.close()
        await project.close()


def main() -> None:
    """Run the Worker until interrupted."""
    asyncio.run(run_worker())


if __name__ == "__main__":
    main()
