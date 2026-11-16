# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The invitee recorder example's mapping, admission rule, binding declaration, and HTTP routing."""

import dataclasses
from datetime import UTC, datetime
from typing import Any

import dex
import httpx
import pytest
from dex_connectors.sdk import TriggerEvent

from dex_connectors.calendly import INVITEE_EVENT_RECEIVED_TRIGGER_DEFINITION, InviteeEvent, new_connection
from dex_connectors.calendly.calendlymock import Mock
from dex_connectors.calendly.triggers import _decode_invitee_webhook
from examples.invitee_recorder.app import InviteeRecorderApplication
from examples.invitee_recorder.flow import (
    CONNECTION_NAME,
    Booking,
    InviteeRecorderFlow,
    accept_booking,
    map_to_flow_input,
    map_to_get_scheduled_event_input,
    resolve_flow_id,
)
from tests.test_invitee_event_received_trigger import CANCELED_BODY, CONNECTION, CREATED_BODY, calendly_client


def decoded(body: str) -> TriggerEvent[InviteeEvent]:
    event = _decode_invitee_webhook(body.encode())
    assert event is not None
    return event


def test_booking_maps_the_verified_invitee_and_names_one_flow_per_event() -> None:
    """The Flow input keeps the invitee's identity, and the Flow ID is the event ID without colons."""
    event = decoded(CREATED_BODY)
    booking = map_to_flow_input(event)
    assert booking == Booking(
        event_id="invitee.created:EVENT0001:INVITEE01",
        invitee_uri="https://api.calendly.com/scheduled_events/EVENT0001/invitees/INVITEE01",
        invitee_email="zoe@example.com",
        scheduled_event_uri="https://api.calendly.com/scheduled_events/EVENT0001",
        booked_at=datetime(2026, 9, 30, 11, 59, tzinfo=UTC),
        invitee_name="Zoë   O'Brien",
    )
    assert resolve_flow_id(event) == "calendly-invitee.created-EVENT0001-INVITEE01"
    encoded = dex.CodecRegistry().resolve(Booking).encode(booking).data
    assert '"bookedAt":"2026-09-30T11:59:00Z"' in encoded
    assert map_to_get_scheduled_event_input(booking).scheduled_event_uri == booking.scheduled_event_uri


def test_only_an_active_new_booking_starts_a_flow() -> None:
    """A cancellation or a canceled invitee is acknowledged without a Flow."""
    created = decoded(CREATED_BODY)
    assert accept_booking(created)
    assert not accept_booking(decoded(CANCELED_BODY))
    canceled_invitee = dataclasses.replace(created.payload.invitee, status="canceled")
    assert not accept_booking(
        TriggerEvent(
            created.id, created.occurred_at, dataclasses.replace(created.payload, invitee=canceled_invitee)
        )
    )


def test_flow_declares_its_binding_with_the_event_type_unit() -> None:
    """Dex Web shows the event type picker, which writes eventTypeUri."""
    flow = InviteeRecorderFlow(new_connection(calendly_client(), CONNECTION))
    (binding,) = flow.get_connector_trigger_bindings()
    assert binding.definition == INVITEE_EVENT_RECEIVED_TRIGGER_DEFINITION
    assert (binding.connection_name, binding.binding_name) == (CONNECTION_NAME, "invitee-created")
    assert binding.configuration_ui is not None
    (unit,) = binding.configuration_ui.units
    assert (unit.unit_id, unit.required) == ("eventTypePicker", False)
    assert "Leave it empty" in unit.description
    assert [(item.port, item.json_pointer) for item in unit.bindings] == [("eventTypeUri", "/eventTypeUri")]
    with pytest.raises(ValueError):
        InviteeRecorderFlow(Mock("another-connection").connection())


async def test_application_routes_the_webhook_and_readiness() -> None:
    """The webhook answers 503 until the binding runs, readiness agrees, and other paths are 404."""

    class StoppedReceiver:
        def running_source_count(self) -> int:
            return 0

        async def __call__(self, scope: Any, receive: Any, send: Any) -> None:
            await send({"type": "http.response.start", "status": 503, "headers": []})
            await send({"type": "http.response.body", "body": b""})

    application = InviteeRecorderApplication(StoppedReceiver())  # type: ignore[arg-type]
    transport = httpx.ASGITransport(app=application)
    async with httpx.AsyncClient(transport=transport, base_url="http://recorder") as client:
        assert (await client.post("/webhooks/calendly", content=b"{}")).status_code == 503
        assert (await client.get("/readyz")).status_code == 503
        assert (await client.get("/other")).status_code == 404
