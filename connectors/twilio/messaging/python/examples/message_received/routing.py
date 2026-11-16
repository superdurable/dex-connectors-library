# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Route the saved messageReceived binding's messages to the inbound message Flow through a durable inbox."""

import dex
from dex_connectors.sdk import TriggerLogger, TriggerTarget, new_dex_flow_trigger_target
from dex_connectors.sdk.pollingtrigger import PollRunner
from dex_connectors.sdk.projectconfig import LoadedProject

from dex_connectors.twilio_messaging import (
    Connection,
    MessageReceivedEvent,
    ProjectMessageReceivedTriggerRoute,
    new_project_message_trigger_runner,
)

from .flow import (
    MESSAGE_RECEIVED_TRIGGER_BINDING,
    InboundMessageFlow,
    accept_every_inbound_message,
    map_to_flow_input,
    resolve_flow_id,
)


def new_inbound_message_target(
    dex_client: dex.AsyncClient, flow: InboundMessageFlow
) -> TriggerTarget[MessageReceivedEvent]:
    """Start one InboundMessageFlow per message; the message's Trigger event ID is the request ID."""
    return new_dex_flow_trigger_target(
        dex_client,
        flow,
        accepts_event=accept_every_inbound_message,
        resolve_flow_id=resolve_flow_id,
        map_to_flow_input=map_to_flow_input,
    )


def open_inbound_message_runner(
    project: LoadedProject,
    connection: Connection,
    dex_client: dex.AsyncClient,
    flow: InboundMessageFlow,
    logger: TriggerLogger | None = None,
) -> PollRunner:
    """Each message is recorded in the binding's project Trigger inbox before the watermark moves past it."""
    route = ProjectMessageReceivedTriggerRoute(
        MESSAGE_RECEIVED_TRIGGER_BINDING, new_inbound_message_target(dex_client, flow)
    )
    return new_project_message_trigger_runner(project, connection, [route], logger)
