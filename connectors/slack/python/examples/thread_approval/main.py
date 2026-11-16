# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Serve the thread approval Worker, the shared Slack Trigger runner, and its relayed route on one event loop.

uvicorn is not a package dependency; run with ``uv run --with uvicorn``.
"""

import asyncio
import logging
import os
import tempfile
from pathlib import Path

import dex
import uvicorn
from dex_connectors.sdk import TriggerContext, TriggerDeliveryAbortedError
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.slack import new_project_connection

from .flow import CONNECTION_NAME, ThreadApprovalFlow
from .receiver import RelayedTriggerRoute, open_thread_approval_runner


async def main() -> None:
    """Load the project, register the Flow, and receive Slack events until the server stops."""
    trigger_logger = logging.getLogger("dex_connectors.sdk.trigger")
    # LOG_LEVEL=DEBUG shows every Slack message the Triggers ignore and every delivery.
    trigger_logger.setLevel(os.environ.get("LOG_LEVEL", "INFO").upper())
    trigger_logger.addHandler(logging.StreamHandler())
    server_address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    flow = ThreadApprovalFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache_directory = Path(tempfile.gettempdir()) / "dex-slack-python-thread-approval-blobs"
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(cache_directory), 1 << 30))
    try:
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
        async with dex.AsyncClient(
            registry, cache, dex.ClientOptions(server_address=server_address)
        ) as client:
            runner = await open_thread_approval_runner(project, client, flow)
            host, _, port = os.environ.get("TRIGGER_BIND_ADDRESS", "127.0.0.1:8839").rpartition(":")
            server = uvicorn.Server(uvicorn.Config(RelayedTriggerRoute(runner), host=host, port=int(port)))
            context = TriggerContext()
            worker_task = asyncio.create_task(worker.start())
            runner_task = asyncio.create_task(runner.run(context))
            # A runner that fails, such as for a refused relay registration, stops the server too.
            runner_task.add_done_callback(lambda _: setattr(server, "should_exit", True))
            try:
                await server.serve()
            finally:
                context.cancellation.cancel()
                (outcome,) = await asyncio.gather(runner_task, return_exceptions=True)
                await worker.stop()
                await worker_task
                await runner.aclose()
            if isinstance(outcome, Exception) and not isinstance(outcome, TriggerDeliveryAbortedError):
                raise outcome
    finally:
        cache.close()
        await connection.aclose()
        await project.close()


if __name__ == "__main__":
    asyncio.run(main())
