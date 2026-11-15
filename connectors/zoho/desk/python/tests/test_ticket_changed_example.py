# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic checks of the ticket change example's Trigger mapping, picker, and Flow wiring."""

from datetime import UTC, datetime

import dex
import pytest
from dex_connectors.sdk import ConnectorUIBinding, TriggerEvent

from dex_connectors.zoho_desk import (
    TICKET_CHANGE_UPDATED,
    TICKET_CHANGED_TRIGGER_DEFINITION,
    GetTicketInput,
    Ticket,
    TicketChangedEvent,
)
from dex_connectors.zoho_desk.deskmock import Mock
from examples.ticket_changed.flow import (
    CONNECTION_NAME,
    TICKET_CHANGED_TRIGGER_BINDING,
    TicketChangedFlow,
    accepts_every_ticket_change,
    map_to_get_ticket_input,
    map_to_ticket_change,
    resolve_ticket_change_flow_id,
)

INSTANT = datetime(2026, 3, 2, 9, 0, 5, tzinfo=UTC)
EVENT = TriggerEvent(
    "zdesk-0123456789abcdef0123456789abcdef",
    INSTANT,
    TicketChangedEvent(
        org_id="2389290",
        department_id="1892000000006907",
        change_kind=TICKET_CHANGE_UPDATED,
        ticket=Ticket(
            id="1892000000099001",
            subject="Billing question",
            status="Open",
            created_at=INSTANT,
            modified_at=INSTANT,
        ),
    ),
)


def test_each_ticket_version_names_one_flow_and_keeps_its_event_id() -> None:
    """A redelivered version finds its Flow; the input carries the event ID beside the change."""
    assert (
        resolve_ticket_change_flow_id(EVENT)
        == "zoho-desk-ticket-change-zdesk-0123456789abcdef0123456789abcdef"
    )
    change = map_to_ticket_change(EVENT)
    assert change.trigger_event_id == EVENT.id
    assert change.change == EVENT.payload
    assert accepts_every_ticket_change(EVENT)
    assert map_to_get_ticket_input(change) == GetTicketInput(ticket_id="1892000000099001")


def test_binding_shows_one_guided_department_picker() -> None:
    """The analyzer reads one static binding whose only visible field is the optional department picker."""
    flow = TicketChangedFlow(Mock(CONNECTION_NAME).connection())
    assert flow.get_flow_type() == "ZohoDeskTicketChangePython"
    [binding] = flow.get_connector_trigger_bindings()
    assert binding.definition == TICKET_CHANGED_TRIGGER_DEFINITION
    assert (binding.connection_name, binding.binding_name) == (
        CONNECTION_NAME,
        TICKET_CHANGED_TRIGGER_BINDING,
    )
    assert binding.configuration_ui is not None
    [unit] = binding.configuration_ui.units
    assert (unit.unit_id, unit.label, unit.required) == ("departmentPicker", "Watched department", False)
    for guidance in (
        "connection's organization",
        "department ID and name",
        "blank",
        "every department",
        "baseline",
        "Restart the Worker",
    ):
        assert guidance in unit.description
    assert unit.bindings == (
        ConnectorUIBinding("departmentId", "/departmentId"),
        ConnectorUIBinding("departmentName", "/departmentName"),
    )


def test_flow_registers_and_requires_its_connection() -> None:
    """The Flow registers with the static connection, and another connection name is refused."""
    dex.Registry([TicketChangedFlow(Mock(CONNECTION_NAME).connection())], allow_async_handlers=True)
    with pytest.raises(ValueError):
        TicketChangedFlow(Mock("other").connection())
