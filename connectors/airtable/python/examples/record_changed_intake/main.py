# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the Worker for the record-changed intake Flow and poll the picked table, one Flow per change.

The Flow reads each changed record back through the same project connection the Trigger polls.

Run with `uv run python -m examples.record_changed_intake.main`.
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

from dex_connectors.airtable import (
    Connection,
    ProjectRecordChangedTriggerRoute,
    new_project_connection,
    new_project_record_changed_trigger_runner,
)

from .flow import (
    CONNECTION_NAME,
    RECORD_CHANGED_TRIGGER_BINDING,
    RecordChangedIntakeFlow,
    accept_changed_record,
    map_to_flow_input,
    resolve_flow_id,
)


def new_record_changed_intake_runner(
    project: LoadedProject, connection: Connection, client: dex.AsyncClient, flow: RecordChangedIntakeFlow
) -> PollRunner:
    """Route the saved binding's changes to the Flow through its durable project inbox and cursor."""
    target = new_dex_flow_trigger_target(
        client,
        flow,
        accepts_event=accept_changed_record,
        resolve_flow_id=resolve_flow_id,
        map_to_flow_input=map_to_flow_input,
    )
    # One durable runner seeds the cursor, records each change in the binding's project inbox, and starts
    # one Flow per change; it logs every failed poll and delivery.
    return new_project_record_changed_trigger_runner(
        project, connection, [ProjectRecordChangedTriggerRoute(RECORD_CHANGED_TRIGGER_BINDING, target)]
    )


async def run() -> None:
    """Serve the Worker and poll the binding until interrupted."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    flow = RecordChangedIntakeFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
    cache_directory = os.environ.get(
        "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-airtable-python-record-changed-blobs")
    )
    cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 30))
    try:
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
            runner = new_record_changed_intake_runner(project, connection, client, flow)
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
