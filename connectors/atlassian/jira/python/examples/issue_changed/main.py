# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the Jira issue change Worker and poll the saved issueChanged binding on one event loop."""

import asyncio
import contextlib
import logging
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk import TriggerCancellation, TriggerContext, TriggerDeliveryAbortedError
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.atlassian_jira import new_project_connection

from .flow import CONNECTION_NAME, IssueChangeFlow
from .trigger import open_issue_trigger_runner


async def main() -> None:
    """Load the project, register the Flow, and start one Flow per issue change until interrupted."""
    trigger_logger = logging.getLogger("dex_connectors.sdk.trigger")
    trigger_logger.setLevel(os.environ.get("LOG_LEVEL", "INFO").upper())
    trigger_logger.addHandler(logging.StreamHandler())
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    flow = IssueChangeFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache_directory = os.environ.get(
        "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-jira-python-issue-changed-blobs")
    )
    cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 30))
    address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
    try:
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
            # The binding's project inbox records each change before its poll cursor moves past it.
            runner = await open_issue_trigger_runner(project, client, flow)
            cancellation = TriggerCancellation()
            worker_task = asyncio.create_task(worker.start())
            try:
                await runner.run(TriggerContext(cancellation))
            finally:
                cancellation.cancel()
                await worker.stop()
                with contextlib.suppress(TriggerDeliveryAbortedError):
                    await worker_task
                await runner.aclose()
    finally:
        cache.close()
        await connection.aclose()
        await project.close()


if __name__ == "__main__":
    with contextlib.suppress(KeyboardInterrupt, TriggerDeliveryAbortedError):
        asyncio.run(main())
