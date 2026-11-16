# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Serve the profile-change Flow and poll the connected member's own LinkedIn profile, one Flow per change."""

import asyncio
import contextlib
import logging
import os
import signal
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk import (
    TriggerCancellation,
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerLogger,
    TriggerTarget,
    new_dex_flow_trigger_target,
)
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.linkedin import (
    ProfileChangedEvent,
    ProjectProfileChangedTriggerRoute,
    new_project_connection,
    new_project_profile_trigger_runner,
)

from .flow import (
    BINDING_NAME,
    CONNECTION_NAME,
    ProfileChangeFlow,
    accepts_every_profile_change,
    map_to_flow_input,
    resolve_flow_id,
)


def new_profile_change_flow_target(
    dex_client: dex.AsyncClient, flow: ProfileChangeFlow, logger: TriggerLogger | None = None
) -> TriggerTarget[ProfileChangedEvent]:
    """Start one ProfileChangeFlow per change, named by its stable Trigger event ID."""
    return new_dex_flow_trigger_target(
        dex_client,
        flow,
        accepts_event=accepts_every_profile_change,
        resolve_flow_id=resolve_flow_id,
        map_to_flow_input=map_to_flow_input,
        logger=logger,
    )


async def run_worker() -> None:
    """Load the named LinkedIn connection, serve the Flow, and run the Trigger until SIGINT or SIGTERM."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    cache_directory = os.environ.get(
        "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-linkedin-profile-changed-python-blobs")
    )
    cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 30))
    address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
    cancellation = TriggerCancellation()
    loop = asyncio.get_running_loop()
    for stop_signal in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(stop_signal, cancellation.cancel)
    try:
        flow = ProfileChangeFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            client_options = dex.ClientOptions(server_address=address)
            async with dex.AsyncClient(registry, cache, client_options) as dex_client:
                # Each change is recorded in the binding's inbox before the stored profile moves past it.
                runner = new_project_profile_trigger_runner(
                    project,
                    connection,
                    [
                        ProjectProfileChangedTriggerRoute(
                            BINDING_NAME, new_profile_change_flow_target(dex_client, flow)
                        )
                    ],
                )
                logging.getLogger(__name__).info(
                    "LinkedIn profile change worker starting: connection %s, binding %s",
                    CONNECTION_NAME,
                    BINDING_NAME,
                )
                with contextlib.suppress(TriggerDeliveryAbortedError):
                    await runner.run(TriggerContext(cancellation))
        finally:
            await worker.stop()
            await worker_task
    finally:
        cache.close()
        await connection.aclose()
        await project.close()


def main() -> None:
    """Run the Worker from the Python module entrypoint, with Trigger records at INFO."""
    logging.basicConfig(level=os.environ.get("LOG_LEVEL", "INFO").upper())
    asyncio.run(run_worker())


if __name__ == "__main__":
    main()
