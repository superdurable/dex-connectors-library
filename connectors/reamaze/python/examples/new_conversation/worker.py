# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Serve the new conversation Flow and poll the watched Re:amaze channel, one Flow per new conversation."""

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

from dex_connectors.reamaze import (
    ConversationCreatedEvent,
    ProjectConversationCreatedTriggerRoute,
    ProjectConversationTriggerRunnerConfig,
    new_project_connection,
    new_project_conversation_trigger_runner,
)

from .flow import (
    BINDING_NAME,
    CONNECTION_NAME,
    NewConversationFlow,
    accept_customer_conversation,
    map_to_flow_input,
    resolve_flow_id,
)


def new_conversation_flow_target(
    dex_client: dex.AsyncClient, flow: NewConversationFlow, logger: TriggerLogger | None = None
) -> TriggerTarget[ConversationCreatedEvent]:
    """Start one NewConversationFlow per conversation that is not spam, named by its Trigger event ID."""
    return new_dex_flow_trigger_target(
        dex_client,
        flow,
        accepts_event=accept_customer_conversation,
        resolve_flow_id=resolve_flow_id,
        map_to_flow_input=map_to_flow_input,
        logger=logger,
    )


async def run_worker() -> None:
    """Load the named Re:amaze connection, serve the Flow, and run the Trigger until SIGINT or SIGTERM."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    cache_directory = os.environ.get(
        "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-reamaze-new-conversation-python-blobs")
    )
    cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 30))
    address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
    cancellation = TriggerCancellation()
    loop = asyncio.get_running_loop()
    for stop_signal in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(stop_signal, cancellation.cancel)
    try:
        flow = NewConversationFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            client_options = dex.ClientOptions(server_address=address)
            async with dex.AsyncClient(registry, cache, client_options) as dex_client:
                # Each conversation is recorded in the binding's inbox before the cursor moves past it.
                route = ProjectConversationCreatedTriggerRoute(
                    BINDING_NAME, new_conversation_flow_target(dex_client, flow)
                )
                runner = new_project_conversation_trigger_runner(
                    project, connection, ProjectConversationTriggerRunnerConfig([route])
                )
                logging.getLogger(__name__).info(
                    "Re:amaze new conversation worker starting: connection %s, binding %s",
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
