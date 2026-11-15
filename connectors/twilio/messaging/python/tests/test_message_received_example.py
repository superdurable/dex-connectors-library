# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic checks of the messageReceived example's mapping, validation, and static binding."""

from datetime import UTC, datetime

import dex
from dex_connectors.sdk import TriggerEvent

from dex_connectors.twilio_messaging import GetMessageInput, Message, MessageReceivedEvent
from dex_connectors.twilio_messaging.messagingmock import Mock
from examples.message_received.flow import (
    CONNECTION_NAME,
    FLOW_TYPE,
    MESSAGE_RECEIVED_TRIGGER_BINDING,
    InboundMessageFlow,
    InboundMessageInput,
    _invalid_input_reason,
    accept_every_inbound_message,
    is_opt_out_text,
    map_to_flow_input,
    map_to_get_message_input,
    resolve_flow_id,
)

SID = "SM" + "0123456789abcdef0123456789abcdef"
RECEIVED = MessageReceivedEvent(
    message=Message(sid=SID, to="+14155550100", from_="+14155550123", direction="inbound", status="received"),
    body="hello",
)
TRIGGER_EVENT = TriggerEvent("twilio-message-" + SID, datetime(2026, 11, 15, 12, tzinfo=UTC), RECEIVED)


def test_each_message_names_its_flow_and_reads_its_message_back() -> None:
    assert resolve_flow_id(TRIGGER_EVENT) == "twilio-inbound-message-twilio-message-" + SID
    assert accept_every_inbound_message(TRIGGER_EVENT)
    flow_input = map_to_flow_input(TRIGGER_EVENT)
    assert flow_input == InboundMessageInput("twilio-message-" + SID, RECEIVED)
    assert map_to_get_message_input(flow_input) == GetMessageInput(SID)


def test_opt_out_keywords_match_the_whole_trimmed_text() -> None:
    for keyword in ("STOP", " stop ", "Unsubscribe", "optout"):
        assert is_opt_out_text(keyword), keyword
    for text in ("please stop", "hello", ""):
        assert not is_opt_out_text(text), text


def test_record_step_refuses_incomplete_deliveries() -> None:
    assert _invalid_input_reason(InboundMessageInput("twilio-message-1", RECEIVED)) == ""
    assert _invalid_input_reason(InboundMessageInput(" ", RECEIVED)) == "triggerEventId is required"
    missing = MessageReceivedEvent(message=Message(sid=SID, direction="inbound"))
    assert "received.message.to" in _invalid_input_reason(InboundMessageInput("twilio-message-1", missing))
    outbound = MessageReceivedEvent(message=Message(sid=SID, to="+14155550100", direction="outbound-api"))
    assert "inbound" in _invalid_input_reason(InboundMessageInput("twilio-message-1", outbound))


def test_flow_declares_the_static_binding_with_the_receiving_address_unit() -> None:
    flow = InboundMessageFlow(Mock(CONNECTION_NAME).connection())
    dex.Registry([flow], allow_async_handlers=True)
    assert flow.get_flow_type() == FLOW_TYPE
    [binding] = flow.get_connector_trigger_bindings()
    assert binding.definition.trigger.trigger_name == "messageReceived"
    assert (binding.connection_name, binding.binding_name) == (
        CONNECTION_NAME,
        MESSAGE_RECEIVED_TRIGGER_BINDING,
    )
    assert binding.configuration_ui is not None
    [unit] = binding.configuration_ui.units
    assert (unit.id, unit.unit_id, unit.label, unit.required) == (
        "receivingNumber",
        "receivingAddressInput",
        "Receiving number",
        False,
    )
    assert [(item.port, item.json_pointer) for item in unit.bindings] == [("to", "/to")]
    assert "https://console.twilio.com/us1/develop/phone-numbers/manage/incoming" in unit.description
    assert "blank" in unit.description and "baseline" in unit.description
