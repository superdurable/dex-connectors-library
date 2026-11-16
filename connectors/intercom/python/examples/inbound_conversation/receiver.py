# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Route the stored conversationEvent binding's notifications to the Flow through its durable inbox."""

import dex
import httpx
from dex_connectors.sdk import TriggerLogger, TriggerTarget, new_dex_flow_trigger_target
from dex_connectors.sdk.projectconfig import LoadedProject

from dex_connectors.intercom import (
    ConversationEvent,
    ConversationEventEndpointRunner,
    ProjectConversationEventTriggerRoute,
    new_project_conversation_event_endpoint_runner,
)

from .flow import (
    BINDING_NAME,
    CONNECTION_NAME,
    InboundConversationFlow,
    accept_inbound_conversation,
    map_to_flow_input,
    resolve_flow_id,
)


def new_inbound_conversation_flow_target(
    dex_client: dex.AsyncClient, flow: InboundConversationFlow, logger: TriggerLogger | None = None
) -> TriggerTarget[ConversationEvent]:
    """Start one InboundConversationFlow per new conversation, named by its conversation ID."""
    return new_dex_flow_trigger_target(
        dex_client,
        flow,
        accepts_event=accept_inbound_conversation,
        resolve_flow_id=resolve_flow_id,
        map_to_flow_input=map_to_flow_input,
        logger=logger,
    )


async def open_inbound_conversation_receiver(
    project: LoadedProject,
    dex_client: dex.AsyncClient,
    flow: InboundConversationFlow,
    http_client: httpx.AsyncClient | None = None,
) -> ConversationEventEndpointRunner:
    """Create the ASGI webhook receiver; close it with ``aclose`` after the server stops.

    Each notification is recorded in the binding's inbox before Intercom receives 200, so it starts its
    Flow even when Dex was unreachable when it arrived.
    """
    return await new_project_conversation_event_endpoint_runner(
        project,
        CONNECTION_NAME,
        [
            ProjectConversationEventTriggerRoute(
                BINDING_NAME, new_inbound_conversation_flow_target(dex_client, flow)
            )
        ],
        http_client,
    )
