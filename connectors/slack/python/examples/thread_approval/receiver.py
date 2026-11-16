# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Route the stored root and reply bindings to the thread approval Flow through one shared runner."""

from typing import Any

import dex
import httpx
from dex_connectors.sdk import new_dex_flow_trigger_target, new_dex_rpc_trigger_target
from dex_connectors.sdk.projectconfig import LoadedProject, TriggerInboxKey, decode_trigger_configuration

from dex_connectors.slack import (
    MessageTriggerRunner,
    ProjectChannelThreadCreatedTriggerRoute,
    ProjectThreadReplyCreatedTriggerRoute,
    SlackTriggerOptions,
    decode_channel_thread_created_trigger_configuration,
    decode_thread_reply_created_trigger_configuration,
    new_project_message_trigger_runner,
    relayed_trigger_path,
)

from .flow import (
    CONNECTION_NAME,
    REPLY_TRIGGER_BINDING,
    START_TRIGGER_BINDING,
    ThreadApprovalFlow,
    map_to_flow_input,
    map_to_record_thread_reply_input,
    new_reply_trigger_filter,
    new_start_trigger_filter,
    resolve_flow_id,
)


async def open_thread_approval_runner(
    project: LoadedProject,
    dex_client: dex.AsyncClient,
    flow: ThreadApprovalFlow,
    http_client: httpx.AsyncClient | None = None,
    options: SlackTriggerOptions | None = None,
) -> MessageTriggerRunner:
    """Run both bindings on one runner, because Slack sends each event to only one Socket Mode connection.

    Before either Dex call, each target applies a filter built from the saved binding configuration.
    Close the runner with ``aclose``.
    """
    start_configuration = decode_trigger_configuration(
        project.configuration,
        TriggerInboxKey("slack", CONNECTION_NAME, "channelThreadCreated", START_TRIGGER_BINDING),
        decode_channel_thread_created_trigger_configuration,
    )
    reply_configuration = decode_trigger_configuration(
        project.configuration,
        TriggerInboxKey("slack", CONNECTION_NAME, "threadReplyCreated", REPLY_TRIGGER_BINDING),
        decode_thread_reply_created_trigger_configuration,
    )
    logger = options.logger if options is not None else None
    start = new_dex_flow_trigger_target(
        dex_client,
        flow,
        accepts_event=new_start_trigger_filter(start_configuration),
        resolve_flow_id=resolve_flow_id,
        map_to_flow_input=map_to_flow_input,
        logger=logger,
    )
    reply = new_dex_rpc_trigger_target(
        dex_client,
        flow.record_thread_reply,
        accepts_event=new_reply_trigger_filter(reply_configuration),
        resolve_flow_id=resolve_flow_id,
        map_to_rpc_input=map_to_record_thread_reply_input,
        logger=logger,
    )
    return await new_project_message_trigger_runner(
        project,
        CONNECTION_NAME,
        channel_thread_created_routes=[ProjectChannelThreadCreatedTriggerRoute(START_TRIGGER_BINDING, start)],
        thread_reply_created_routes=[ProjectThreadReplyCreatedTriggerRoute(REPLY_TRIGGER_BINDING, reply)],
        http_client=http_client,
        options=options,
    )


class RelayedTriggerRoute:
    """An ASGI application serving the runner at its relayed path and 404 everywhere else."""

    def __init__(self, runner: MessageTriggerRunner) -> None:
        self._runner = runner
        self._path = relayed_trigger_path(CONNECTION_NAME)

    async def __call__(self, scope: Any, receive: Any, send: Any) -> None:
        """Serve one ASGI scope; lifespan events are acknowledged and the runner runs separately."""
        if scope["type"] == "lifespan":
            await receive()
            await send({"type": "lifespan.startup.complete"})
            await receive()
            await send({"type": "lifespan.shutdown.complete"})
            return
        if scope["type"] == "http" and scope["path"] == self._path:
            await self._runner(scope, receive, send)
            return
        await send({"type": "http.response.start", "status": 404, "headers": []})
        await send({"type": "http.response.body", "body": b""})
