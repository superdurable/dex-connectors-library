# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the Slack channel history Worker with a Dex project connection."""

import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.slack import new_project_connection

from .flow import CONNECTION_NAME, ChannelMessagesFlow


async def run() -> None:
    """Load the project OAuth connection and serve the example Flow."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    cache = dex.open_blob_cache(
        dex.BlobCacheConfig(str(Path(tempfile.gettempdir()) / "dex-slack-python-channel-blobs"), 1 << 30)
    )
    try:
        registry = dex.Registry([ChannelMessagesFlow(connection)], allow_async_handlers=True)
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
