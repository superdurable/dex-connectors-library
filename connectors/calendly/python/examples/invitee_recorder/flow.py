# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""One Flow per booked Calendly invitee: record the booking and read its scheduled event back."""

from dataclasses import dataclass, field
from datetime import datetime

import dex
from dex_connectors.sdk import (
    ConnectorConfigurationUI,
    ConnectorUIBinding,
    ConnectorUIUnit,
    QueryResult,
    TriggerBindingDefinition,
    TriggerEvent,
)

from dex_connectors.calendly import (
    WEBHOOK_EVENT_INVITEE_CREATED,
    Connection,
    GetScheduledEventInput,
    InviteeEvent,
    ScheduledEvent,
    define_invitee_event_received_trigger_binding,
    new_get_scheduled_event_step,
)

CONNECTION_NAME = "calendly-scheduling"
"""Static Dex Web connection that verifies webhooks and reads scheduled events."""
BINDING_NAME = "invitee-created"
"""The inviteeEventReceived binding whose invitee.created events start this Flow."""
FLOW_TYPE = "CalendlyInviteeRecorderPython"
FLOW_ID_PREFIX = "calendly-"
"""Precedes the Trigger event ID in every Flow ID, so a redelivered webhook maps to one Flow."""


@dataclass(frozen=True, slots=True)
class Booking:
    """The Flow's start input: one verified invitee.created webhook, without the invitee's answers."""

    event_id: str = field(metadata={"dex_json_name": "eventId"})
    invitee_uri: str = field(metadata={"dex_json_name": "inviteeUri"})
    invitee_email: str = field(metadata={"dex_json_name": "inviteeEmail"})
    scheduled_event_uri: str = field(metadata={"dex_json_name": "scheduledEventUri"})
    booked_at: datetime = field(metadata={"dex_json_name": "bookedAt", "dex_json_format": "rfc3339"})
    invitee_name: str = field(
        default="", metadata={"dex_json_name": "inviteeName", "dex_json_omit_default": True}
    )


@dataclass(frozen=True, slots=True)
class RecordedScheduledEvent:
    """The found getScheduledEvent outcome the Flow stores and returns."""

    branch: str
    scheduled_event: ScheduledEvent = field(metadata={"dex_json_name": "scheduledEvent"})


booking_attribute = dex.Attribute("calendly-booking", Booking)
scheduled_event_attribute = dex.Attribute("calendly-scheduled-event", RecordedScheduledEvent)


def accept_booking(event: TriggerEvent[InviteeEvent]) -> bool:
    """Admit only an active invitee.created event; the binding already filters by event type."""
    return event.payload.event == WEBHOOK_EVENT_INVITEE_CREATED and event.payload.invitee.status == "active"


def resolve_flow_id(event: TriggerEvent[InviteeEvent]) -> str:
    """Derive the Flow ID from the event ID, so every redelivery maps to one Flow."""
    return FLOW_ID_PREFIX + event.id.replace(":", "-")


def map_to_flow_input(event: TriggerEvent[InviteeEvent]) -> Booking:
    """Copy the verified booking into the Flow's start input."""
    invitee = event.payload.invitee
    return Booking(
        event_id=event.id,
        invitee_uri=invitee.uri,
        invitee_email=invitee.email,
        scheduled_event_uri=invitee.scheduled_event_uri,
        booked_at=event.payload.created_at,
        invitee_name=invitee.name,
    )


def map_to_get_scheduled_event_input(booking: Booking) -> GetScheduledEventInput:
    """Read the event the invitee booked."""
    return GetScheduledEventInput(scheduled_event_uri=booking.scheduled_event_uri)


# dex:group group-id:booking group-label:"Booking"
# dex:explanation text:"Persist the verified booking before reading its scheduled event."
class RecordBooking(dex.Step[Booking]):
    """Persist the verified booking."""

    def __init__(self, read_scheduled_event: dex.StepTarget[Booking]) -> None:
        """Bind the read-back Step this Step continues to."""
        self.read_scheduled_event = read_scheduled_event

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "RecordBooking"

    def execute(self, context: dex.Context, input: Booking) -> dex.StepDecision:
        """Store the booking and read its scheduled event."""
        if not input.event_id or not input.scheduled_event_uri:
            return dex.force_fail("a booking requires its event ID and scheduled event URI")
        booking_attribute.set(context, input)
        return dex.go_to(self.read_scheduled_event, input)


# dex:group group-id:calendly group-label:"Calendly"
# dex:explanation text:"Persist the scheduled event Calendly returned and complete the Flow."
class RecordScheduledEvent(dex.Step[QueryResult[ScheduledEvent]]):
    """Persist the found scheduled event and complete."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "RecordScheduledEvent"

    def execute(self, context: dex.Context, input: QueryResult[ScheduledEvent]) -> dex.StepDecision:
        """Store the event and complete the Flow with it."""
        recorded = RecordedScheduledEvent(input.branch, input.value)
        scheduled_event_attribute.set(context, recorded)
        return dex.graceful_complete(recorded)


class InviteeRecorderFlow(dex.Flow[Booking]):
    """Record one booked Calendly invitee and the scheduled event it booked."""

    def __init__(self, connection: Connection) -> None:
        """Bind the static Calendly connection at Worker registration."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError(f"invitee recorder Flow requires the {CONNECTION_NAME} connection")
        self.record_scheduled_event = RecordScheduledEvent()
        self.read_scheduled_event = new_get_scheduled_event_step(
            step_type="ReadScheduledEvent",
            step_input_type=Booking,
            annotations={
                "groupId": "calendly",
                "groupLabel": "Calendly",
                "explanation": "Read the booked scheduled event back from Calendly by its URI.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_get_scheduled_event_input,
            found=self.record_scheduled_event,
        )
        self.record_booking = RecordBooking(self.read_scheduled_event)

    def get_flow_type(self) -> str:
        """Return the stable Flow identity shown in Dex Web."""
        return "CalendlyInviteeRecorderPython"

    def get_steps(self) -> dex.StepList[Booking]:
        """Start at the record Step, which the Trigger starts the Flow at."""
        return dex.StepList.start_step(self.record_booking).other_steps(
            self.read_scheduled_event, self.record_scheduled_event
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the booking and scheduled-event Attributes."""
        return dex.PersistenceSchema.of(booking_attribute, scheduled_event_attribute)

    # Dex Web shows the event type unit for this binding; the analyzer requires a lone return statement.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_invitee_event_received_trigger_binding(
                connection_name="calendly-scheduling",
                binding_name="invitee-created",
                configuration_ui=ConnectorConfigurationUI(
                    (
                        ConnectorUIUnit(
                            "eventType",
                            "eventTypePicker",
                            "Event type",
                            "Select the Calendly event type whose new bookings start this Flow; the picker "
                            "lists the connected user's event types and stores the event type URI. Leave it "
                            "empty to record bookings of every event type.",
                            False,
                            (ConnectorUIBinding("eventTypeUri", "/eventTypeUri"),),
                        ),
                    )
                ),
            )
        ]
