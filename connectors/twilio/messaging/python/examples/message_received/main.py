# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the messageReceived example Worker and poll the configured Twilio number until interrupted."""

import asyncio
import logging
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk import TriggerContext, TriggerDeliveryAbortedError
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.twilio_messaging import new_project_connection

from .flow import CONNECTION_NAME, InboundMessageFlow
from .routing import open_inbound_message_runner


async def main() -> None:
    """Load the project, register the Flow, and start one Flow per inbound message until cancelled."""
    trigger_logger = logging.getLogger("dex_connectors.sdk.trigger")
    trigger_logger.setLevel(logging.INFO)
    trigger_logger.addHandler(logging.StreamHandler())
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    flow = InboundMessageFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache_directory = os.environ.get(
        "DEX_BLOB_CACHE_DIR",
        str(Path(tempfile.gettempdir()) / "dex-twilio-python-message-received-blobs"),
    )
    cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 30))
    address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
    try:
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as dex_client:
            runner = open_inbound_message_runner(project, connection, dex_client, flow)
            context = TriggerContext()
            worker_task = asyncio.create_task(worker.start())
            try:
                await runner.run(context)
            except TriggerDeliveryAbortedError:
                pass
            finally:
                context.cancellation.cancel()
                await worker.stop()
                await worker_task
    finally:
        cache.close()
        await connection.aclose()
        await project.close()


if __name__ == "__main__":
    asyncio.run(main())
