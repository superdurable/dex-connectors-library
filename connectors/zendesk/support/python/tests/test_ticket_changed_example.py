# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic checks of the ticket change example's Trigger mapping and Flow wiring."""

from datetime import UTC, datetime

import pytest
from dex_connectors.sdk import TriggerEvent

from dex_connectors.zendesk_support import TICKET_CHANGE_CREATED, Ticket, TicketChangedEvent
from dex_connectors.zendesk_support.supportmock import Mock
from examples.ticket_changed.flow import (
    CONNECTION_NAME,
    TICKET_CHANGED_TRIGGER_BINDING,
    TicketChangedFlow,
    accepts_every_ticket_change,
    map_to_get_ticket_input,
    map_to_ticket_change,
    resolve_ticket_change_flow_id,
)

EVENT = TriggerEvent(
    "zendesk-ticket-897cba421f2a136d36a4375ed6386bdb",
    datetime(2026, 3, 2, 8, 58, tzinfo=UTC),
    TicketChangedEvent(change_kind=TICKET_CHANGE_CREATED, ticket=Ticket(id=42, status="new")),
)


def test_each_ticket_version_names_one_flow_and_reads_its_newest_comment() -> None:
    """A redelivered version finds its Flow; the read asks for one comment."""
    assert resolve_ticket_change_flow_id(EVENT) == f"zendesk-ticket-change-{EVENT.id}"
    change = map_to_ticket_change(EVENT)
    assert change.trigger_event_id == EVENT.id
    assert change.change == EVENT.payload
    assert accepts_every_ticket_change(EVENT)
    read = map_to_get_ticket_input(change)
    assert (read.ticket_id, read.latest_comment_limit) == (42, 1)


def test_flow_declares_the_static_binding_and_requires_its_connection() -> None:
    """The analyzer reads one static binding, and another connection name is refused."""
    flow = TicketChangedFlow(Mock(CONNECTION_NAME).connection())
    assert flow.get_flow_type() == "ZendeskTicketChangePython"
    [binding] = flow.get_connector_trigger_bindings()
    assert (binding.connection_name, binding.binding_name) == (
        CONNECTION_NAME,
        TICKET_CHANGED_TRIGGER_BINDING,
    )
    assert binding.configuration_ui is None
    with pytest.raises(ValueError):
        TicketChangedFlow(Mock("other").connection())
