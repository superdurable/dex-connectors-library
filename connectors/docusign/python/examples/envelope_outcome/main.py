# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Serve the Connect endpoint and run the Worker that records each verified envelope outcome.

Run with `uv run --with uvicorn python -m examples.envelope_outcome.main`.
"""

import asyncio
import importlib
import logging
import os
import tempfile
from pathlib import Path
from typing import Any

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.docusign import new_project_connection

from .flow import CONNECTION_NAME, EnvelopeOutcomeFlow
from .receiver import ConnectWebhookApplication, new_envelope_outcome_endpoint_runner


async def run() -> None:
    """Serve Connect and the Worker until interrupted; uvicorn's lifespan runs the binding."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    flow = EnvelopeOutcomeFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
    directory = os.environ.get(
        "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-docusign-envelope-outcome-blobs")
    )
    cache = dex.open_blob_cache(dex.BlobCacheConfig(directory, 1 << 30))
    try:
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        client_options = dex.ClientOptions(server_address=address)
        async with dex.AsyncClient(registry, cache, client_options) as client:
            runner = new_envelope_outcome_endpoint_runner(project, connection, client, flow)
            uvicorn: Any = importlib.import_module("uvicorn")
            host, _, port = os.environ.get("CONNECT_BIND_ADDRESS", "127.0.0.1:8862").rpartition(":")
            application = ConnectWebhookApplication(runner)
            server = uvicorn.Server(uvicorn.Config(application, host=host, port=int(port)))
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
    """Log Trigger delivery at INFO and run until interrupted."""
    logging.basicConfig(level=os.environ.get("LOG_LEVEL", "INFO").upper())
    logging.getLogger("dex_connectors.sdk.trigger").setLevel(logging.INFO)
    asyncio.run(run())


if __name__ == "__main__":
    main()
