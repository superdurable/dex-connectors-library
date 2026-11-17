# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the ClickUp webhook and Dex Worker with `uv run --with uvicorn python -m examples.task_event.main`."""

import asyncio
import importlib
import logging
import os
import tempfile
from pathlib import Path
from typing import Any

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.clickup import new_project_connection

from .flow import CONNECTION_NAME, TaskEventFlow
from .receiver import TaskEventWebhookApplication, new_task_event_endpoint_runner


async def run() -> None:
    """Serve webhook delivery and the Worker until interrupted."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    flow = TaskEventFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
    directory = os.environ.get(
        "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-clickup-task-event-blobs")
    )
    cache = dex.open_blob_cache(dex.BlobCacheConfig(directory, 1 << 30))
    try:
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
            runner = new_task_event_endpoint_runner(project, connection, client, flow)
            uvicorn: Any = importlib.import_module("uvicorn")
            host, _, port = os.environ.get("CLICKUP_BIND_ADDRESS", "127.0.0.1:8864").rpartition(":")
            server = uvicorn.Server(
                uvicorn.Config(TaskEventWebhookApplication(runner), host=host, port=int(port))
            )
            worker_task = asyncio.create_task(worker.start())
            try:
                await server.serve()
            finally:
                await worker.stop()
                await worker_task
    finally:
        cache.close()
        await connection.aclose()
        await project.close()


def main() -> None:
    """Enable delivery logging and run the application."""
    logging.basicConfig(level=os.environ.get("LOG_LEVEL", "INFO").upper())
    logging.getLogger("dex_connectors.sdk.trigger").setLevel(logging.INFO)
    asyncio.run(run())


if __name__ == "__main__":
    main()
