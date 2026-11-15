# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the file intake Worker and the durable Google Drive file Trigger runner on one event loop."""

import asyncio
import logging
import signal

import dex
from dex_connectors.sdk import TriggerContext, TriggerDeliveryAbortedError
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.google_drive import new_project_connection

from .flow import CONNECTION_NAME, FileIntakeFlow
from .intake import new_file_intake_target, open_file_intake_runner
from .worker import dex_server_address, open_registry_and_cache


async def main() -> None:
    """Load the project, serve the Flow, and start one Flow per created file until SIGINT or SIGTERM."""
    trigger_logger = logging.getLogger("dex_connectors.sdk.trigger")
    trigger_logger.setLevel(logging.INFO)
    trigger_logger.addHandler(logging.StreamHandler())
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    flow = FileIntakeFlow(connection)
    registry, cache = open_registry_and_cache(flow)
    context = TriggerContext()
    loop = asyncio.get_running_loop()
    for stop_signal in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(stop_signal, context.cancellation.cancel)
    try:
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=dex_server_address()))
        client_options = dex.ClientOptions(server_address=dex_server_address())
        async with dex.AsyncClient(registry, cache, client_options) as client:
            runner = await open_file_intake_runner(project, new_file_intake_target(client, flow))
            worker_task = asyncio.create_task(worker.start())
            try:
                await runner.run(context)
            except TriggerDeliveryAbortedError:
                pass  # SIGINT or SIGTERM cancelled the runner.
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
