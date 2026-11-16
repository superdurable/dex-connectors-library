# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic checks of the ticket_changed example's Flow wiring and mappers."""

from dataclasses import replace
from datetime import UTC, datetime

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, StaticCredentialProvider, TriggerEvent

from dex_connectors.gorgias import Client, Config, TicketChangedEvent, new_connection
from examples.ticket_changed.flow import (
    BINDING_NAME,
    CONNECTION_NAME,
    TicketChangeFlow,
    TicketChangeInput,
    accepts_every_ticket_change,
    map_to_flow_input,
    map_to_get_ticket_input,
    resolve_flow_id,
    validate_ticket_change_input,
)


def ticket_event(event_type: str = "ticket-closed") -> TriggerEvent[TicketChangedEvent]:
    """One delivered event."""
    payload = TicketChangedEvent(
        event_id=9003,
        event_type=event_type,
        ticket_id=7002,
        occurred_at=datetime(2026, 11, 2, 9, tzinfo=UTC),
        user_id=12,
    )
    return TriggerEvent("gorgias-event-9003", payload.occurred_at, payload)


def test_mappers_name_one_flow_per_gorgias_event_and_read_its_ticket() -> None:
    event = ticket_event()
    assert resolve_flow_id(event) == "gorgias-ticket-change-gorgias-event-9003"
    flow_input = map_to_flow_input(event)
    assert flow_input == TicketChangeInput("gorgias-event-9003", event.payload)
    assert map_to_get_ticket_input(flow_input).ticket_id == 7002
    assert accepts_every_ticket_change(ticket_event("ticket-created"))


def test_validation_refuses_an_incomplete_event() -> None:
    valid = map_to_flow_input(ticket_event())
    assert validate_ticket_change_input(valid) == ""
    assert "triggerEventId" in validate_ticket_change_input(replace(valid, trigger_event_id=" "))
    assert "eventId" in validate_ticket_change_input(replace(valid, event=replace(valid.event, event_id=0)))
    assert "ticketId" in validate_ticket_change_input(replace(valid, event=replace(valid.event, ticket_id=0)))
    message_event = replace(valid.event, event_type="ticket-message-created")
    assert "eventType" in validate_ticket_change_input(replace(valid, event=message_event))


async def test_flow_registers_and_declares_its_trigger_binding_without_picker_units() -> None:
    async with httpx.AsyncClient() as http_client:
        client = Client(Config(domain="acme"), StaticCredentialProvider({}), http_client)
        flow = TicketChangeFlow(new_connection(client, ConnectionRef("gorgias", CONNECTION_NAME)))
        dex.Registry([flow], allow_async_handlers=True)
        assert flow.get_flow_type() == "GorgiasTicketChangePython"
        [binding] = flow.get_connector_trigger_bindings()
        assert (binding.connection_name, binding.binding_name) == (CONNECTION_NAME, BINDING_NAME)
        assert binding.definition.trigger.trigger_name == "ticketChanged"
        assert binding.configuration_ui is None
        with pytest.raises(ValueError, match=CONNECTION_NAME):
            TicketChangeFlow(new_connection(client, ConnectionRef("gorgias", "other")))
