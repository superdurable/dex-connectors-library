# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Serve the Intercom webhook receiver and the Flow's Dex Worker on one event loop.

uvicorn is not a package dependency; run with ``uv run --with uvicorn``.
"""

import asyncio
import logging
import os
import tempfile
from pathlib import Path
from typing import Any

import dex
import uvicorn
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.intercom import ConversationEventEndpointRunner, new_project_connection

from .flow import CONNECTION_NAME, InboundConversationFlow
from .receiver import open_inbound_conversation_receiver

WEBHOOK_PATH = "/webhooks/intercom"
READINESS_PATH = "/readyz"


class InboundConversationRoutes:
    """Serve the webhook at WEBHOOK_PATH and a readiness check at READINESS_PATH."""

    def __init__(self, receiver: ConversationEventEndpointRunner) -> None:
        """Route to the receiver, which also runs the binding through the ASGI lifespan."""
        self.receiver = receiver

    async def __call__(self, scope: Any, receive: Any, send: Any) -> None:
        """Pass lifespan and the webhook path to the receiver; answer readiness and 404 here."""
        if scope["type"] != "http" or scope.get("path") == WEBHOOK_PATH:
            await self.receiver(scope, receive, send)
            return
        is_ready = scope.get("path") == READINESS_PATH and self.receiver.running_source_count() > 0
        status = 200 if is_ready else 503 if scope.get("path") == READINESS_PATH else 404
        await send({"type": "http.response.start", "status": status, "headers": []})
        await send({"type": "http.response.body", "body": b"ready\n" if is_ready else b""})


async def main() -> None:
    """Load the project, register the Flow, and receive Intercom notifications until shutdown."""
    logging.basicConfig(level=os.environ.get("LOG_LEVEL", "INFO").upper())
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
    cache_directory = os.environ.get(
        "DEX_BLOB_CACHE_DIR",
        str(Path(tempfile.gettempdir()) / "dex-intercom-inbound-conversation-python-blobs"),
    )
    cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 30))
    try:
        flow = InboundConversationFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as dex_client:
            receiver = await open_inbound_conversation_receiver(project, dex_client, flow)
            host, _, port = os.environ.get("INTERCOM_WEBHOOK_BIND_ADDRESS", "127.0.0.1:8852").rpartition(":")
            config = uvicorn.Config(
                InboundConversationRoutes(receiver), host=host, port=int(port), lifespan="on"
            )
            worker_task = asyncio.create_task(worker.start())
            try:
                await uvicorn.Server(config).serve()
            finally:
                await worker.stop()
                await worker_task
                await receiver.aclose()
    finally:
        cache.close()
        await connection.aclose()
        await project.close()


if __name__ == "__main__":
    asyncio.run(main())
