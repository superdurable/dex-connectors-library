# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic checks of the new_conversation_intake example's Flow wiring, mappers, and binding UI."""

from datetime import UTC, datetime

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, StaticCredentialProvider, TriggerEvent

from dex_connectors.hiver import (
    Client,
    Config,
    Conversation,
    ConversationReceivedEvent,
    new_connection,
)
from examples.new_conversation_intake.flow import (
    CONNECTION_NAME,
    CONVERSATION_RECEIVED_TRIGGER_BINDING,
    NewConversationIntakeFlow,
    ReceivedConversation,
    accept_every_conversation,
    map_to_flow_input,
    map_to_get_conversation_input,
    resolve_flow_id,
    validate_received_conversation,
)

DETECTED_AT = datetime(2036, 2, 23, 9, 30, tzinfo=UTC)


def delivery(conversation_id: str = "573741003") -> TriggerEvent[ConversationReceivedEvent]:
    """One delivered conversation."""
    payload = ConversationReceivedEvent(
        inbox_id="105902", conversation=Conversation(conversation_id, "105902", status="open")
    )
    return TriggerEvent(f"conversation-105902-{conversation_id}", DETECTED_AT, payload)


def test_mappers_name_one_flow_per_conversation_and_read_it_back() -> None:
    event = delivery()
    assert resolve_flow_id(event) == "hiver-new-conversation-conversation-105902-573741003"
    flow_input = map_to_flow_input(event)
    assert flow_input == ReceivedConversation(event.id, event.payload, DETECTED_AT)
    request = map_to_get_conversation_input(flow_input)
    assert (request.inbox_id, request.conversation_id) == ("105902", "573741003")
    assert accept_every_conversation(event)


def test_validation_refuses_an_unusable_delivery() -> None:
    assert validate_received_conversation(map_to_flow_input(delivery())) == ""
    assert validate_received_conversation(ReceivedConversation(" ", delivery().payload, DETECTED_AT)) != ""
    assert validate_received_conversation(map_to_flow_input(delivery(conversation_id=" "))) != ""


async def test_flow_registers_and_declares_its_trigger_binding_with_the_inbox_picker() -> None:
    async with httpx.AsyncClient() as http_client:
        client = Client(Config(), StaticCredentialProvider({}), http_client)
        flow = NewConversationIntakeFlow(new_connection(client, ConnectionRef("hiver", CONNECTION_NAME)))
        dex.Registry([flow], allow_async_handlers=True)
        assert flow.get_flow_type() == "HiverNewConversationIntakePython"
        [binding] = flow.get_connector_trigger_bindings()
        assert (binding.connection_name, binding.binding_name) == (
            CONNECTION_NAME,
            CONVERSATION_RECEIVED_TRIGGER_BINDING,
        )
        assert binding.definition.trigger.trigger_name == "conversationReceived"
        assert binding.configuration_ui is not None
        [unit] = binding.configuration_ui.units
        assert (unit.id, unit.unit_id, unit.label, unit.required) == (
            "sharedInbox",
            "inboxPicker",
            "Shared inbox",
            True,
        )
        assert [(item.port, item.json_pointer) for item in unit.bindings] == [("inboxId", "/inboxId")]
        for guidance in ("https://api2.hiverhq.com/v1/inboxes", "105902", "not secret", "cannot be blank"):
            assert guidance in unit.description
        with pytest.raises(ValueError, match=CONNECTION_NAME):
            NewConversationIntakeFlow(new_connection(client, ConnectionRef("hiver", "other")))
