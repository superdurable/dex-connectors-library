# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Poll the picked Hiver shared inbox and run the Worker that starts one Flow per new conversation."""

import asyncio
import logging
import os
import signal
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk import TriggerContext, TriggerDeliveryAbortedError
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.hiver import new_project_connection

from .flow import CONNECTION_NAME, NewConversationIntakeFlow
from .intake import new_conversation_target, open_conversation_intake_runner

_LOG_LEVELS = {"debug": logging.DEBUG, "info": logging.INFO, "warn": logging.WARNING, "error": logging.ERROR}


def configure_logging() -> None:
    """Write Trigger records at LOG_LEVEL: debug, info, warn, or error, with info by default."""
    logging.basicConfig(level=logging.WARNING, format="%(asctime)s %(levelname)s %(name)s %(message)s")
    level_name = os.environ.get("LOG_LEVEL", "").strip().lower()
    # The poll runner, the durable inbox, and the Flow target all log to this logger.
    logging.getLogger("dex_connectors.sdk.trigger").setLevel(_LOG_LEVELS.get(level_name, logging.INFO))
    if level_name and level_name not in _LOG_LEVELS:
        logging.getLogger(__name__).warning("LOG_LEVEL is not debug, info, warn, or error; using info")


async def main() -> None:
    """Load the project, start the Worker, and poll the binding until SIGINT or SIGTERM."""
    configure_logging()
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    flow = NewConversationIntakeFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache_directory = os.environ.get(
        "DEX_BLOB_CACHE_DIR",
        str(Path(tempfile.gettempdir()) / "dex-hiver-python-new-conversation-intake-blobs"),
    )
    cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 30))
    address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
    context = TriggerContext()
    loop = asyncio.get_running_loop()
    for stop_signal in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(stop_signal, context.cancellation.cancel)
    try:
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
            runner = open_conversation_intake_runner(
                project, connection, new_conversation_target(client, flow)
            )
            worker_task = asyncio.create_task(worker.start())
            try:
                await worker.wait_pull_registered(timeout=60)
                # Replays the inbox, records the baseline on the first start, then polls until a stop signal.
                await runner.run(context)
            except TriggerDeliveryAbortedError:
                pass
            finally:
                await worker.stop()
                await worker_task
    finally:
        cache.close()
        await connection.aclose()
        await project.close()


if __name__ == "__main__":
    asyncio.run(main())
