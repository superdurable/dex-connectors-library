# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the Gmail thread reply Worker and the shared Gmail message Trigger runner."""

import asyncio
import contextlib
import tempfile
from collections.abc import Callable
from pathlib import Path

import dex
from dex_connectors.sdk import (
    TriggerCancellation,
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerLogger,
    TriggerTarget,
    bind_trigger_log_attributes,
    new_dex_flow_trigger_target,
    new_dex_rpc_trigger_target,
)
from dex_connectors.sdk.projectconfig import LoadedProject, TriggerInboxKey, decode_trigger_configuration

from dex_connectors.google_gmail import (
    CONNECTOR_ID,
    Connection,
    MessageEvent,
    ProjectMessageReceivedTriggerRoute,
    ProjectReplyReceivedTriggerRoute,
    decode_message_received_trigger_configuration,
    decode_reply_received_trigger_configuration,
    new_project_message_trigger_runner,
)

from .flow import (
    CONNECTION_NAME,
    REPLY_TRIGGER_BINDING,
    START_TRIGGER_BINDING,
    GmailThreadReplyFlow,
    map_to_flow_input,
    map_to_receive_email_reply_input,
    new_reply_trigger_filter,
    new_start_trigger_filter,
    resolve_flow_id,
)


def new_thread_reply_targets(
    project: LoadedProject,
    dex_client: dex.AsyncClient,
    flow: GmailThreadReplyFlow,
    logger: TriggerLogger | None = None,
) -> tuple[TriggerTarget[MessageEvent], TriggerTarget[MessageEvent]]:
    """Build the root Flow-start target and the reply RPC target, filtered by the saved bindings."""
    start_configuration = decode_trigger_configuration(
        project.configuration,
        TriggerInboxKey(CONNECTOR_ID, CONNECTION_NAME, "messageReceived", START_TRIGGER_BINDING),
        decode_message_received_trigger_configuration,
    )
    reply_configuration = decode_trigger_configuration(
        project.configuration,
        TriggerInboxKey(CONNECTOR_ID, CONNECTION_NAME, "replyReceived", REPLY_TRIGGER_BINDING),
        decode_reply_received_trigger_configuration,
    )

    def binding_logger(trigger: str, binding: str) -> TriggerLogger:
        attributes = {
            "connector": CONNECTOR_ID,
            "connection": CONNECTION_NAME,
            "trigger": trigger,
            "binding": binding,
        }
        return bind_trigger_log_attributes(logger, attributes)

    start = new_dex_flow_trigger_target(
        dex_client,
        flow,
        accepts_event=new_start_trigger_filter(start_configuration),
        resolve_flow_id=resolve_flow_id,
        map_to_flow_input=map_to_flow_input,
        logger=binding_logger("messageReceived", START_TRIGGER_BINDING),
    )
    reply = new_dex_rpc_trigger_target(
        dex_client,
        flow.receive_email_reply,
        accepts_event=new_reply_trigger_filter(reply_configuration),
        resolve_flow_id=resolve_flow_id,
        map_to_rpc_input=map_to_receive_email_reply_input,
        logger=binding_logger("replyReceived", REPLY_TRIGGER_BINDING),
    )
    return start, reply


async def run_thread_reply_application(
    project: LoadedProject,
    connection: Connection,
    server_address: str,
    cancellation: TriggerCancellation,
    *,
    logger: TriggerLogger | None = None,
    on_ready: Callable[[dex.AsyncClient, GmailThreadReplyFlow], None] | None = None,
) -> None:
    """Serve the Flow on a pull Worker and run both Gmail bindings on one runner until cancellation.

    connection is the `gmail-inbox` connection opened from project; the caller keeps owning it. on_ready
    is called once the Worker is registered, before the runner first polls.
    """
    flow = GmailThreadReplyFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    with tempfile.TemporaryDirectory(prefix="dex-gmail-trigger-thread-reply-") as directory:
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 64 * 1024 * 1024))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=30)
            options = dex.ClientOptions(server_address=server_address)
            async with dex.AsyncClient(registry, cache, options) as dex_client:
                start, reply = new_thread_reply_targets(project, dex_client, flow, logger)
                # One ordered runner delivers every root before any reply of the same poll.
                runner = new_project_message_trigger_runner(
                    project,
                    connection,
                    message_received_routes=[
                        ProjectMessageReceivedTriggerRoute(START_TRIGGER_BINDING, start)
                    ],
                    reply_received_routes=[ProjectReplyReceivedTriggerRoute(REPLY_TRIGGER_BINDING, reply)],
                )
                if on_ready is not None:
                    on_ready(dex_client, flow)
                with contextlib.suppress(TriggerDeliveryAbortedError):
                    await runner.run(TriggerContext(cancellation))
        finally:
            await worker.stop()
            await worker_task
            cache.close()
