# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic checks of the tag_new_conversations example's admission, mapping, and binding."""

from datetime import UTC, datetime

import pytest
from dex_connectors.sdk import ConnectionRef, TriggerEvent

from dex_connectors.helpscout import CONVERSATION_EVENT_TRIGGER_DEFINITION, Conversation, ConversationEvent
from dex_connectors.helpscout.helpscoutmock import Mock
from examples.tag_new_conversations.flow import (
    CONNECTION_NAME,
    NEW_CONVERSATION_TRIGGER_BINDING,
    NewConversationInput,
    TagNewConversationFlow,
    accept_new_conversation,
    map_to_flow_input,
    resolve_flow_id,
)

EVENT_ID = "convo.created:501:0f1e2d3c4b5a69788796a5b4c3d2e1f0"


def conversation_event(
    event: str = "convo.created", status: str = "active"
) -> TriggerEvent[ConversationEvent]:
    """One delivered conversation webhook."""
    conversation = Conversation(501, 12, "Double charge", status=status, mailbox_id=123)
    return TriggerEvent(
        EVENT_ID, datetime(2026, 9, 30, 12, tzinfo=UTC), ConversationEvent(event, conversation=conversation)
    )


def test_only_a_new_active_conversation_starts_the_flow() -> None:
    assert accept_new_conversation(conversation_event())
    assert not accept_new_conversation(conversation_event("convo.tags"))
    assert not accept_new_conversation(conversation_event(status="pending"))


def test_the_flow_id_and_input_come_from_the_trigger_event() -> None:
    event = conversation_event()
    assert resolve_flow_id(event) == "helpscout-convo.created-501-0f1e2d3c4b5a69788796a5b4c3d2e1f0"
    assert map_to_flow_input(event) == NewConversationInput(EVENT_ID, "convo.created", 501, 123)


def test_the_binding_shows_only_the_optional_inbox_picker() -> None:
    flow = TagNewConversationFlow(Mock(CONNECTION_NAME).connection())
    assert flow.get_flow_type() == "HelpScoutTagNewConversationPython"
    [binding] = flow.get_connector_trigger_bindings()
    assert binding.definition == CONVERSATION_EVENT_TRIGGER_DEFINITION
    assert (binding.connection_name, binding.binding_name) == (
        CONNECTION_NAME,
        NEW_CONVERSATION_TRIGGER_BINDING,
    )
    assert binding.configuration_ui is not None
    [unit] = binding.configuration_ui.units
    assert (unit.id, unit.unit_id, unit.label, unit.required) == ("inbox", "mailboxPicker", "Inbox", False)
    assert "Leave it empty" in unit.description and "inbox ID" in unit.description
    assert [(port.port, port.json_pointer) for port in unit.bindings] == [("mailboxId", "/mailboxId")]


def test_the_flow_requires_the_static_connection() -> None:
    with pytest.raises(ValueError, match="helpscout-support"):
        TagNewConversationFlow(Mock("another").connection())
    assert ConnectionRef("helpscout", CONNECTION_NAME).name == CONNECTION_NAME
