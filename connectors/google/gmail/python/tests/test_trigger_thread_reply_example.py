# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic checks of the trigger_thread_reply example's Flow wiring, mappers, and filters."""

from datetime import UTC, datetime

import dex
import pytest
from dex_connectors.sdk import TriggerEvent

from dex_connectors.google_gmail import (
    MessageEvent,
    MessageMatcher,
    MessageReceivedTriggerConfiguration,
    ReplyReceivedTriggerConfiguration,
    new_connection_with_operations,
)
from dex_connectors.google_gmail.gmailmock import Mock
from examples.trigger_thread_reply.flow import (
    CONNECTION_NAME,
    REPLY_TEXT,
    REPLY_TRIGGER_BINDING,
    START_TRIGGER_BINDING,
    GmailThreadReplyFlow,
    ReceiveEmailReplyInput,
    ThreadReplyInput,
    ThreadState,
    map_to_flow_input,
    map_to_get_message_input,
    map_to_receive_email_reply_input,
    map_to_reply_input,
    new_reply_trigger_filter,
    new_start_trigger_filter,
    resolve_flow_id,
)


def event(message_id: str = "m1", *, is_reply: bool = False, **payload: object) -> TriggerEvent[MessageEvent]:
    """One delivered Gmail message event."""
    values: dict[str, object] = {
        "primary_email": "owner@example.com",
        "message_id": message_id,
        "thread_id": "t1",
        "from_": "Ada <ada@example.com>",
        "subject": "Order 1042",
        "snippet": "Approved, thanks",
        "received_at": datetime(2026, 1, 2, tzinfo=UTC),
        "is_reply": is_reply,
        **payload,
    }
    return TriggerEvent(message_id, datetime(2026, 1, 2, tzinfo=UTC), MessageEvent(**values))  # type: ignore[arg-type]


def test_mappers_share_one_flow_per_thread_and_carry_only_ids() -> None:
    root = event("root")
    reply = event("reply", is_reply=True)
    assert resolve_flow_id(root) == resolve_flow_id(reply) == "gmail-thread-reply-gmail-inbox-t1"
    flow_input = map_to_flow_input(root)
    assert flow_input == ThreadReplyInput("root", "owner@example.com", "root", "t1")
    assert map_to_get_message_input(flow_input).message_id == "root"
    assert map_to_receive_email_reply_input(reply) == ReceiveEmailReplyInput("reply", "reply")
    state = ThreadState(flow_input, "replying", reply_event_id="reply", reply_message_id="reply")
    reply_input = map_to_reply_input(state)
    assert (reply_input.message_id, reply_input.text_body) == ("reply", REPLY_TEXT)


def test_filters_check_the_shape_sender_and_text_again() -> None:
    start = new_start_trigger_filter(
        MessageReceivedTriggerConfiguration(message_matcher=MessageMatcher("order", ("ada@example.com",)))
    )
    assert start(event())
    assert not start(event(is_reply=True))
    assert not start(event(from_="Eve <eve@example.com>"))
    assert not start(event(subject="Hello", snippet="hi"))
    assert not start(event(thread_id=""))
    reply = new_reply_trigger_filter(ReplyReceivedTriggerConfiguration())
    assert reply(event(is_reply=True))
    assert not reply(event())
    assert not reply(event(is_reply=True, primary_email=""))


def test_flow_registers_and_declares_both_trigger_bindings() -> None:
    flow = GmailThreadReplyFlow(Mock(CONNECTION_NAME).connection)
    dex.Registry([flow], allow_async_handlers=True)
    assert flow.get_flow_type() == "GmailThreadReplyPython"
    start, reply = flow.get_connector_trigger_bindings()
    assert (start.binding_name, start.definition.trigger.trigger_name) == (
        START_TRIGGER_BINDING,
        "messageReceived",
    )
    assert (reply.binding_name, reply.definition.trigger.trigger_name) == (
        REPLY_TRIGGER_BINDING,
        "replyReceived",
    )
    for binding in (start, reply):
        assert binding.connection_name == CONNECTION_NAME
        assert binding.configuration_ui is not None
        units = binding.configuration_ui.units
        assert [unit.unit_id for unit in units] == ["searchQueryInput", "textInput", "emailListInput"]
        assert all(unit.description and not unit.required for unit in units)
    mock = Mock("other").connection
    other = new_connection_with_operations(
        mock.get_message(), mock.send_message(), mock.reply_to_message(), mock.reference
    )
    with pytest.raises(ValueError, match=CONNECTION_NAME):
        GmailThreadReplyFlow(other)
