# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the Worker for the databaseRowChanged example Flow and poll the picked data source, one Flow per
row version.

Run with `uv run python -m examples.row_changed.main`.
"""

import asyncio
import logging
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk import TriggerContext, TriggerDeliveryAbortedError, new_dex_flow_trigger_target
from dex_connectors.sdk.pollingtrigger import PollRunner
from dex_connectors.sdk.projectconfig import LoadedProject, load_from_environment

from dex_connectors.notion import (
    Connection,
    ProjectDatabaseRowChangedTriggerRoute,
    ProjectDatabaseRowTriggerRunnerConfig,
    new_project_connection,
    new_project_database_row_trigger_runner,
)

from .flow import (
    CONNECTION_NAME,
    DATABASE_ROW_CHANGED_TRIGGER_BINDING,
    DatabaseRowChangeFlow,
    accept_every_row_change,
    map_to_flow_input,
    resolve_flow_id,
)


def new_row_change_trigger_runner(
    project: LoadedProject, connection: Connection, client: dex.AsyncClient, flow: DatabaseRowChangeFlow
) -> PollRunner:
    """Route the saved binding's row changes to the Flow through its durable project inbox and cursor."""
    target = new_dex_flow_trigger_target(
        client,
        flow,
        accepts_event=accept_every_row_change,
        resolve_flow_id=resolve_flow_id,
        map_to_flow_input=map_to_flow_input,
    )
    # Each row change is recorded in the binding's project Trigger inbox before the cursor moves past it.
    return new_project_database_row_trigger_runner(
        project,
        connection,
        ProjectDatabaseRowTriggerRunnerConfig(
            [ProjectDatabaseRowChangedTriggerRoute(DATABASE_ROW_CHANGED_TRIGGER_BINDING, target)]
        ),
    )


async def run() -> None:
    """Serve the Worker and poll the binding until interrupted."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    flow = DatabaseRowChangeFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
    cache_directory = os.environ.get(
        "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-notion-python-row-changed-blobs")
    )
    cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 30))
    try:
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
            runner = new_row_change_trigger_runner(project, connection, client, flow)
            worker_task = asyncio.create_task(worker.start())
            try:
                await runner.run(TriggerContext())
            except TriggerDeliveryAbortedError:
                pass
            finally:
                await worker.stop()
                await worker_task
    finally:
        cache.close()
        await connection.aclose()
        await project.close()


def main() -> None:
    """Log poll and Trigger delivery records at LOG_LEVEL (INFO by default) and run until interrupted."""
    logging.basicConfig(level=os.environ.get("LOG_LEVEL", "INFO").upper())
    asyncio.run(run())


if __name__ == "__main__":
    main()
