# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Route the stored conversationReceived binding's conversations to the intake Flow."""

import dex
from dex_connectors.sdk import TriggerLogger, TriggerTarget, new_dex_flow_trigger_target
from dex_connectors.sdk.pollingtrigger import PollRunner
from dex_connectors.sdk.projectconfig import LoadedProject

from dex_connectors.hiver import (
    Connection,
    ConversationReceivedEvent,
    ProjectConversationReceivedTriggerRoute,
    new_project_conversation_received_trigger_runner,
)

from .flow import (
    CONVERSATION_RECEIVED_TRIGGER_BINDING,
    NewConversationIntakeFlow,
    accept_every_conversation,
    map_to_flow_input,
    resolve_flow_id,
)


def new_conversation_target(
    dex_client: dex.AsyncClient, flow: NewConversationIntakeFlow, logger: TriggerLogger | None = None
) -> TriggerTarget[ConversationReceivedEvent]:
    """Start one intake Flow per new conversation, with the Trigger event ID as the Flow ID suffix."""
    return new_dex_flow_trigger_target(
        dex_client,
        flow,
        accepts_event=accept_every_conversation,
        resolve_flow_id=resolve_flow_id,
        map_to_flow_input=map_to_flow_input,
        logger=logger,
    )


def open_conversation_intake_runner(
    project: LoadedProject,
    connection: Connection,
    target: TriggerTarget[ConversationReceivedEvent],
    logger: TriggerLogger | None = None,
) -> PollRunner:
    """Create the durable runner for the stored binding.

    It records the baseline, then records each new conversation in the binding's project inbox before the
    cursor moves past it and hands it to target.
    """
    return new_project_conversation_received_trigger_runner(
        project,
        connection,
        [ProjectConversationReceivedTriggerRoute(CONVERSATION_RECEIVED_TRIGGER_BINDING, target)],
        logger=logger,
    )
