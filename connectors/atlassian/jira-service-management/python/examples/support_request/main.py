# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Serve Jira customer lookup and support ticket creation Flows."""

import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.atlassian_jira_service_management import new_project_connection

from .flow import CONNECTION_NAME, CreateTicketFlow, SupportRequestFlow


async def run() -> None:
    """Load the connection and run the Dex Worker."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    cache = dex.open_blob_cache(
        dex.BlobCacheConfig(str(Path(tempfile.gettempdir()) / "dex-jsm-python-blobs"), 1 << 30)
    )
    try:
        registry = dex.Registry(
            [SupportRequestFlow(connection), CreateTicketFlow(connection)], allow_async_handlers=True
        )
        async with dex.AsyncWorker(
            registry,
            cache,
            dex.WorkerOptions(server_address=os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")),
        ) as worker:
            await worker.start()
    finally:
        cache.close()
        await connection.aclose()
        await project.close()


if __name__ == "__main__":
    asyncio.run(run())
