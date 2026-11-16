# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Serve the Help Scout webhook and the tagging Flow's Dex Worker on one event loop.

uvicorn is not a package dependency; run with ``uv run --with uvicorn``.
"""

import asyncio
import logging
import os
import tempfile
from pathlib import Path

import dex
import uvicorn
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.helpscout import new_project_connection

from .flow import CONNECTION_NAME, TagNewConversationFlow
from .receiver import WEBHOOK_PATH, open_new_conversation_receiver


async def main() -> None:
    """Load the project, register the Flow, and receive Help Scout webhooks until shutdown."""
    logging.basicConfig(level=os.environ.get("LOG_LEVEL", "INFO").upper())
    server_address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    flow = TagNewConversationFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache_directory = os.environ.get(
        "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-helpscout-tag-new-conversations")
    )
    cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 30))
    try:
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
        client_options = dex.ClientOptions(server_address=server_address)
        async with dex.AsyncClient(registry, cache, client_options) as dex_client:
            receiver = await open_new_conversation_receiver(project, dex_client, flow)
            host, _, port = os.environ.get("WEBHOOK_BIND_ADDRESS", "127.0.0.1:8872").rpartition(":")
            # The receiver answers every path; a reverse proxy routes only WEBHOOK_PATH to it.
            logging.getLogger(__name__).info(
                "Help Scout webhook listening at http://%s:%s%s", host, port, WEBHOOK_PATH
            )
            server = uvicorn.Server(uvicorn.Config(receiver, host=host, port=int(port), lifespan="on"))
            worker_task = asyncio.create_task(worker.start())
            try:
                await server.serve()
            finally:
                await worker.stop()
                await worker_task
                await receiver.aclose()
    finally:
        await connection.aclose()
        cache.close()
        await project.close()


if __name__ == "__main__":
    asyncio.run(main())
