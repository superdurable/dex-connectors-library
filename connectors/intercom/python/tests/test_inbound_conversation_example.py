# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic checks of the inbound_conversation example's Flow wiring and mappers."""

import json
from datetime import UTC, datetime

import dex
import pytest
from dex_connectors.sdk import ConnectionRef, TriggerEvent

from dex_connectors.intercom import (
    TOPIC_CONVERSATION_USER_CREATED,
    TOPIC_CONVERSATION_USER_REPLIED,
    UI_CONVERSATION_TOPIC_PICKER_PORT_TOPICS,
    UI_UNIT_CONVERSATION_TOPIC_PICKER,
    Conversation,
    ConversationAuthor,
    ConversationDetails,
    ConversationEvent,
    ConversationSource,
    new_connection,
)
from examples.inbound_conversation.flow import (
    BINDING_NAME,
    CONNECTION_NAME,
    InboundConversation,
    InboundConversationFlow,
    InboundConversationOutcome,
    accept_inbound_conversation,
    choose_inbound_conversation_outcome,
    map_to_flow_input,
    map_to_get_conversation_input,
    resolve_flow_id,
    validate_inbound_conversation,
)
from tests.conversation_event_support import new_client

NOTIFIED_AT = datetime(2026, 1, 1, 1, 1, 40, tzinfo=UTC)


def notification(
    topic: str = TOPIC_CONVERSATION_USER_CREATED, workspace_id: str = "ecahpwf5"
) -> TriggerEvent[ConversationEvent]:
    """One delivered notification for conversation 215468."""
    payload = ConversationEvent(
        notification_id="notif_1",
        topic=topic,
        workspace_id=workspace_id,
        conversation=Conversation(id="215468", state="open"),
        notified_at=NOTIFIED_AT,
    )
    return TriggerEvent("notif_1", NOTIFIED_AT, payload)


def test_mappers_name_one_flow_per_conversation_without_message_text() -> None:
    """The Flow ID is the conversation's, and the start input carries only identities."""
    event = notification()
    assert accept_inbound_conversation(event)
    assert not accept_inbound_conversation(notification(TOPIC_CONVERSATION_USER_REPLIED))
    assert resolve_flow_id(event) == "intercom-inbound-conversation-215468"
    flow_input = map_to_flow_input(event)
    assert flow_input == InboundConversation(
        "215468", "notif_1", TOPIC_CONVERSATION_USER_CREATED, "ecahpwf5", NOTIFIED_AT
    )
    encoded = json.loads(dex.CodecRegistry().resolve(InboundConversation).encode(flow_input).data)
    assert encoded == {
        "conversationId": "215468",
        "notificationId": "notif_1",
        "topic": TOPIC_CONVERSATION_USER_CREATED,
        "workspaceId": "ecahpwf5",
        "notifiedAt": "2026-01-01T01:01:40Z",
    }
    without_workspace = map_to_flow_input(notification(workspace_id=""))
    assert "workspaceId" not in json.loads(
        dex.CodecRegistry().resolve(InboundConversation).encode(without_workspace).data
    )
    read = map_to_get_conversation_input(flow_input)
    assert (read.conversation_id, read.latest_part_limit) == ("215468", 5)


def test_validation_and_outcome() -> None:
    """A Start Flow input needs a numeric conversation ID; the outcome names the starting customer."""
    assert validate_inbound_conversation(InboundConversation("215468")) == ""
    assert "digits" in validate_inbound_conversation(InboundConversation("abc"))
    assert "digits" in validate_inbound_conversation(InboundConversation(""))
    details = ConversationDetails(
        conversation=Conversation(
            id="215468",
            state="closed",
            source=ConversationSource(author=ConversationAuthor(email="jane@acme.example.com")),
        ),
        part_count=3,
    )
    outcome = choose_inbound_conversation_outcome(InboundConversation("215468", "notif_1"), details)
    assert outcome == InboundConversationOutcome("215468", "notif_1", "closed", "jane@acme.example.com", 3)


async def test_flow_registers_and_declares_its_trigger_binding_with_the_topic_picker() -> None:
    """The Flow binds its static connection and declares the conversationEvent binding for Dex Web."""
    client = new_client()
    flow = InboundConversationFlow(new_connection(client, ConnectionRef("intercom", CONNECTION_NAME)))
    dex.Registry([flow], allow_async_handlers=True)
    assert flow.get_flow_type() == "IntercomInboundConversationPython"
    [binding] = flow.get_connector_trigger_bindings()
    assert (binding.connection_name, binding.binding_name) == (CONNECTION_NAME, BINDING_NAME)
    assert binding.definition.trigger.trigger_name == "conversationEvent"
    assert binding.configuration_ui is not None
    [unit] = binding.configuration_ui.units
    assert unit.unit_id == UI_UNIT_CONVERSATION_TOPIC_PICKER and not unit.required
    assert [(item.port, item.json_pointer) for item in unit.bindings] == [
        (UI_CONVERSATION_TOPIC_PICKER_PORT_TOPICS, "/topics")
    ]
    assert "Configure > Webhooks" in unit.description
    with pytest.raises(ValueError, match=CONNECTION_NAME):
        InboundConversationFlow(new_connection(client, ConnectionRef("intercom", "other")))
    await client.aclose()
