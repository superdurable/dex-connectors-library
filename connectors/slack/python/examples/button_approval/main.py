# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Serve the button approval Worker and the shared Slack Socket Mode runner until interrupted."""

import asyncio
import logging
import os
import signal
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk import TriggerContext, TriggerDeliveryAbortedError
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.slack import new_project_connection

from .flow import CONNECTION_NAME, ButtonApprovalFlow
from .receiver import open_button_approval_runner


async def main() -> None:
    """Load the project, register the Flow, and receive Slack events until SIGINT or SIGTERM."""
    trigger_logger = logging.getLogger("dex_connectors.sdk.trigger")
    # LOG_LEVEL=DEBUG shows every Slack message and click the Triggers ignore, with its reason.
    trigger_logger.setLevel(os.environ.get("LOG_LEVEL", "INFO").upper())
    trigger_logger.addHandler(logging.StreamHandler())
    server_address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    flow = ButtonApprovalFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache_directory = Path(tempfile.gettempdir()) / "dex-slack-python-button-approval-blobs"
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(cache_directory), 1 << 30))
    context = TriggerContext()
    loop = asyncio.get_running_loop()
    for stop_signal in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(stop_signal, context.cancellation.cancel)
    try:
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
        async with dex.AsyncClient(
            registry, cache, dex.ClientOptions(server_address=server_address)
        ) as client:
            runner = await open_button_approval_runner(project, client, flow)
            worker_task = asyncio.create_task(worker.start())
            try:
                await runner.run(context)
            except TriggerDeliveryAbortedError:
                pass  # SIGINT or SIGTERM stopped the runner.
            finally:
                await worker.stop()
                await worker_task
                await runner.aclose()
    finally:
        cache.close()
        await connection.aclose()
        await project.close()


if __name__ == "__main__":
    asyncio.run(main())
