# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic checks of the new_conversation example's Flow wiring and mappers."""

from datetime import UTC, datetime

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider, TriggerEvent

from dex_connectors.reamaze import (
    Client,
    Config,
    Conversation,
    ConversationChannel,
    ConversationCreatedEvent,
    ConversationDetails,
    ConversationMessage,
    ConversationParticipant,
    Credentials,
    new_connection,
)
from examples.new_conversation.flow import (
    BINDING_NAME,
    CONNECTION_NAME,
    Intake,
    NewConversationFlow,
    NewConversationInput,
    accept_customer_conversation,
    map_to_flow_input,
    map_to_get_conversation_input,
    new_intake,
    resolve_flow_id,
    validate_new_conversation_input,
)

CREATED_AT = datetime(2026, 3, 2, 9, 0, tzinfo=UTC)


def created(status: int = 0, slug: str = "refund-request") -> TriggerEvent[ConversationCreatedEvent]:
    """One delivered conversation."""
    conversation = Conversation(id=slug, subject="Refund", status=status, created_at=CREATED_AT)
    payload = ConversationCreatedEvent(brand="acme", channel="support", conversation=conversation)
    return TriggerEvent("reamaze-conversation-0123", CREATED_AT, payload)


def test_mappers_name_one_flow_per_conversation_and_read_it_back() -> None:
    event = created()
    assert resolve_flow_id(event) == "reamaze-new-conversation-reamaze-conversation-0123"
    flow_input = map_to_flow_input(event)
    assert flow_input == NewConversationInput("reamaze-conversation-0123", event.payload)
    assert map_to_get_conversation_input(flow_input).conversation_id == "refund-request"


def test_admission_skips_spam_and_keeps_archived_conversations() -> None:
    assert accept_customer_conversation(created(0))
    assert accept_customer_conversation(created(4)), "archived conversations still start a Flow"
    assert not accept_customer_conversation(created(3))
    assert not accept_customer_conversation(created(9))


def test_validation_refuses_an_unusable_delivery() -> None:
    assert validate_new_conversation_input(map_to_flow_input(created())) == ""
    assert "triggerEventId" in validate_new_conversation_input(NewConversationInput(" ", created().payload))
    assert "conversation.id" in validate_new_conversation_input(map_to_flow_input(created(slug=" ")))


def test_intake_summarizes_the_conversation_read_back() -> None:
    conversation = Conversation(
        id="refund-request",
        subject="Refund",
        status=1,
        channel=ConversationChannel(slug="billing", name="Billing", type=1),
        requester=ConversationParticipant(name="Jane", email="jane@example.com"),
        created_at=CREATED_AT,
    )
    messages = (ConversationMessage(body="a", visibility=0), ConversationMessage(body="b", visibility=1))
    details = ConversationDetails(conversation, messages)
    assert new_intake(details) == Intake("refund-request", "billing", 1, 2, "Refund", "jane@example.com")
    assert new_intake(ConversationDetails(Conversation(id="x", status=0))).requester_email == ""


async def test_flow_registers_and_declares_its_trigger_binding() -> None:
    async with httpx.AsyncClient() as http_client:
        reference = ConnectionRef("reamaze", CONNECTION_NAME)
        credentials = StaticCredentialProvider({reference: Credentials("a@example.com", SecretString("x"))})
        client = Client(Config(brand="acme"), credentials, http_client)
        flow = NewConversationFlow(new_connection(client, reference))
        dex.Registry([flow], allow_async_handlers=True)
        assert flow.get_flow_type() == "ReamazeNewConversationPython"
        [binding] = flow.get_connector_trigger_bindings()
        assert (binding.connection_name, binding.binding_name) == (CONNECTION_NAME, BINDING_NAME)
        assert binding.definition.trigger.trigger_name == "conversationCreated"
        assert binding.configuration_ui is not None
        [unit] = binding.configuration_ui.units
        assert unit.unit_id == "channelSlugEntry"
        with pytest.raises(ValueError, match=CONNECTION_NAME):
            NewConversationFlow(new_connection(client, ConnectionRef("reamaze", "other")))
