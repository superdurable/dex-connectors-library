# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic checks of the ticket change example's Trigger mapping and Flow wiring."""

from datetime import UTC, datetime

import pytest
from dex_connectors.sdk import TriggerEvent

from dex_connectors.freshworks_freshdesk import TICKET_CHANGE_CREATED, Ticket, TicketChangedEvent
from dex_connectors.freshworks_freshdesk.freshdeskmock import Mock
from examples.ticket_changed.flow import (
    CONNECTION_NAME,
    TICKET_CHANGED_TRIGGER_BINDING,
    TicketChangedFlow,
    accepts_every_ticket_change,
    map_to_ticket_change,
    resolve_ticket_change_flow_id,
)

EVENT = TriggerEvent(
    "freshdesk-ticket-42-1772442005",
    datetime(2026, 3, 2, 9, 0, 5, tzinfo=UTC),
    TicketChangedEvent(TICKET_CHANGE_CREATED, Ticket(id=42, status=2, priority=1)),
)


def test_each_ticket_version_names_one_flow_and_keeps_its_event_id() -> None:
    """A redelivered version finds its Flow; the input carries the event ID beside the change."""
    assert resolve_ticket_change_flow_id(EVENT) == "freshdesk-ticket-change-freshdesk-ticket-42-1772442005"
    change = map_to_ticket_change(EVENT)
    assert change.trigger_event_id == EVENT.id
    assert change.change == EVENT.payload
    assert accepts_every_ticket_change(EVENT)


def test_flow_declares_the_static_binding_and_requires_its_connection() -> None:
    """The analyzer reads one static binding, and another connection name is refused."""
    flow = TicketChangedFlow(Mock(CONNECTION_NAME).connection())
    assert flow.get_flow_type() == "FreshdeskTicketChangePython"
    [binding] = flow.get_connector_trigger_bindings()
    assert (binding.connection_name, binding.binding_name) == (
        CONNECTION_NAME,
        TICKET_CHANGED_TRIGGER_BINDING,
    )
    with pytest.raises(ValueError):
        TicketChangedFlow(Mock("other").connection())
