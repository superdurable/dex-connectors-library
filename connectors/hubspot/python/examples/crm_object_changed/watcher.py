# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the Flow's Dex Worker and the durable crmObjectChanged poll runner on one event loop."""

import asyncio
import contextlib
import os
import tempfile
from collections.abc import Callable
from pathlib import Path

import dex
import httpx
from dex_connectors.sdk import (
    TriggerCancellation,
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerLogger,
    new_dex_flow_trigger_target,
)
from dex_connectors.sdk.projectconfig import LoadedProject

from dex_connectors.hubspot import (
    ProjectCRMObjectChangedTriggerRoute,
    ProjectCRMObjectTriggerRunner,
    new_project_connection,
    new_project_crm_object_trigger_runner,
)

from .flow import (
    CONNECTION_NAME,
    CRM_OBJECT_CHANGED_TRIGGER_BINDING,
    DELIVERED_PROPERTIES,
    CRMObjectChangeFlow,
    accept_every_crm_object_change,
    map_to_flow_input,
    resolve_flow_id,
)

ReadyCallback = Callable[[dex.AsyncClient, ProjectCRMObjectTriggerRunner], None]


async def run_crm_object_change_watcher(
    project: LoadedProject,
    server_address: str,
    cancellation: TriggerCancellation,
    *,
    http_client: httpx.AsyncClient | None = None,
    logger: TriggerLogger | None = None,
    on_ready: ReadyCallback | None = None,
) -> None:
    """Serve the Flow and poll the binding's saved object type until cancellation, one Flow per change.

    http_client, owned by the caller, replaces the provider transport, as tests do; logger receives the
    Trigger records. on_ready receives the Client and runner once the Worker is registered.
    """
    connection = new_project_connection(project, CONNECTION_NAME, http_client)
    flow = CRMObjectChangeFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache_directory = os.environ.get(
        "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-hubspot-python-crm-object-changed-blobs")
    )
    cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 30))
    try:
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=30)
            async with dex.AsyncClient(
                registry, cache, dex.ClientOptions(server_address=server_address)
            ) as client:
                target = new_dex_flow_trigger_target(
                    client,
                    flow,
                    accepts_event=accept_every_crm_object_change,
                    resolve_flow_id=resolve_flow_id,
                    map_to_flow_input=map_to_flow_input,
                    logger=logger,
                )
                # Each change is recorded in the binding's inbox before the cursor moves past it.
                runner = await new_project_crm_object_trigger_runner(
                    project,
                    CONNECTION_NAME,
                    [
                        ProjectCRMObjectChangedTriggerRoute(
                            CRM_OBJECT_CHANGED_TRIGGER_BINDING, target, DELIVERED_PROPERTIES
                        )
                    ],
                    http_client,
                    logger,
                )
                try:
                    running = asyncio.create_task(runner.run(TriggerContext(cancellation)))
                    if on_ready is not None:
                        on_ready(client, runner)
                    with contextlib.suppress(TriggerDeliveryAbortedError):
                        await running
                finally:
                    await runner.aclose()
        finally:
            await worker.stop()
            await worker_task
    finally:
        cache.close()
        await connection.aclose()
