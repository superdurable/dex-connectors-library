# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Route the stored new-conversations binding to the tagging Flow through its durable inbox."""

import dex
import httpx
from dex_connectors.sdk import TriggerLogger, new_dex_flow_trigger_target
from dex_connectors.sdk.projectconfig import LoadedProject

from dex_connectors.helpscout import (
    ConversationEventEndpointRunner,
    ProjectConversationEventTriggerRoute,
    new_project_conversation_event_endpoint_runner,
)

from .flow import (
    CONNECTION_NAME,
    NEW_CONVERSATION_TRIGGER_BINDING,
    TagNewConversationFlow,
    accept_new_conversation,
    map_to_flow_input,
    resolve_flow_id,
)

WEBHOOK_PATH = "/webhooks/helpscout"
"""The Help Scout webhook's callback path; expose it through an HTTPS tunnel in local development."""


async def open_new_conversation_receiver(
    project: LoadedProject,
    dex_client: dex.AsyncClient,
    flow: TagNewConversationFlow,
    http_client: httpx.AsyncClient | None = None,
    logger: TriggerLogger | None = None,
) -> ConversationEventEndpointRunner:
    """Create the ASGI webhook endpoint runner; close it with ``aclose`` after the server stops."""
    target = new_dex_flow_trigger_target(
        dex_client,
        flow,
        accepts_event=accept_new_conversation,
        resolve_flow_id=resolve_flow_id,
        map_to_flow_input=map_to_flow_input,
        logger=logger,
    )
    route = ProjectConversationEventTriggerRoute(NEW_CONVERSATION_TRIGGER_BINDING, target)
    return await new_project_conversation_event_endpoint_runner(
        project, CONNECTION_NAME, [route], http_client, logger
    )
