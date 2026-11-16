# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""One Flow per Gorgias ticket event: record it and read the ticket back with its customer and messages."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import QueryResult, TriggerBindingDefinition, TriggerEvent

from dex_connectors.gorgias import (
    TICKET_EVENT_TYPES,
    Connection,
    GetTicketInput,
    TicketChangedEvent,
    TicketDetails,
    define_ticket_changed_trigger_binding,
    new_get_ticket_step,
)

CONNECTION_NAME = "gorgias-ticket-watcher"
BINDING_NAME = "helpdesk-ticket-events"
FLOW_TYPE = "GorgiasTicketChangePython"


@dataclass(frozen=True, slots=True)
class TicketChangeInput:
    """One ticketChanged delivery: the stable Trigger event ID beside the delivered event."""

    trigger_event_id: str = field(metadata={"dex_json_name": "triggerEventId"})
    event: TicketChangedEvent


@dataclass(frozen=True, slots=True)
class TicketChangeOutcome:
    """The Flow result: the changed ticket, the event type, and the ticket read back from Gorgias."""

    ticket_id: int = field(metadata={"dex_json_name": "ticketId"})
    event_type: str = field(metadata={"dex_json_name": "eventType"})
    current: TicketDetails


ticket_change_attribute = dex.Attribute("gorgias-ticket-change", TicketChangeInput)
ticket_change_outcome_attribute = dex.Attribute("gorgias-ticket-change-outcome", TicketChangeOutcome)


def resolve_flow_id(event: TriggerEvent[TicketChangedEvent]) -> str:
    """Name one Flow per Gorgias event, so a redelivered event finds its existing Flow."""
    return f"gorgias-ticket-change-{event.id}"


def map_to_flow_input(event: TriggerEvent[TicketChangedEvent]) -> TicketChangeInput:
    """Keep the Trigger event ID beside the delivered event."""
    return TicketChangeInput(event.id, event.payload)


def accepts_every_ticket_change(event: TriggerEvent[TicketChangedEvent]) -> bool:
    """Admit every event; an application narrows it, for example to rule-made events."""
    return True


def map_to_get_ticket_input(input: TicketChangeInput) -> GetTicketInput:
    """Read the ticket the event names."""
    return GetTicketInput(ticket_id=input.event.ticket_id)


def validate_ticket_change_input(input: TicketChangeInput) -> str:
    """Return why a delivered event is unusable, or "" when it is valid."""
    if not input.trigger_event_id.strip():
        return "triggerEventId is required"
    if input.event.event_id < 1:
        return "event.eventId is required"
    if input.event.ticket_id < 1:
        return "event.ticketId is required"
    if input.event.event_type not in TICKET_EVENT_TYPES:
        return "event.eventType must be a ticket event type that ticketChanged reads"
    return ""


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record the ticket read back from Gorgias as the outcome and complete the Flow."
class CompleteTicketChange(dex.Step[QueryResult[TicketDetails]]):
    """Record the read-back outcome and complete."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "CompleteTicketChange"

    def execute(self, context: dex.Context, input: QueryResult[TicketDetails]) -> dex.StepDecision:
        """Keep the ticket Gorgias returned beside the delivered event type."""
        change = ticket_change_attribute.get(context)
        outcome = TicketChangeOutcome(change.event.ticket_id, change.event.event_type, input.value)
        ticket_change_outcome_attribute.set(context, outcome)
        return dex.graceful_complete(outcome)


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record the delivered ticket event before reading the ticket back."
class RecordTicketChange(dex.Step[TicketChangeInput]):
    """Validate and record the delivered event."""

    def __init__(self, read_changed_ticket: dex.StepTarget[TicketChangeInput]) -> None:
        """Bind the read-back Step this Step continues to."""
        self.read_changed_ticket = read_changed_ticket

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "RecordTicketChange"

    def execute(self, context: dex.Context, input: TicketChangeInput) -> dex.StepDecision:
        """Fail an unusable event; otherwise record it and read the ticket back."""
        problem = validate_ticket_change_input(input)
        if problem:
            return dex.force_fail(problem)
        ticket_change_attribute.set(context, input)
        return dex.go_to(self.read_changed_ticket, input)


class TicketChangeFlow(dex.Flow[TicketChangeInput]):
    """Record one Gorgias ticket event and read the ticket back."""

    def __init__(self, connection: Connection) -> None:
        """Bind the static Gorgias connection at Worker registration."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError(f"ticket change Flow requires the {CONNECTION_NAME} connection")
        self.complete = CompleteTicketChange()
        self.read_changed_ticket = new_get_ticket_step(
            step_type="ReadChangedTicket",
            step_input_type=TicketChangeInput,
            annotations={
                "groupId": "change",
                "groupLabel": "Change",
                "explanation": "Read the ticket back with its customer and latest messages, which the "
                "event names only by ID.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_get_ticket_input,
            found=self.complete,
        )
        self.record = RecordTicketChange(self.read_changed_ticket)

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "GorgiasTicketChangePython"

    def get_steps(self) -> dex.StepList[TicketChangeInput]:
        """Start at the record Step, which the Trigger starts the Flow at."""
        return dex.StepList.start_step(self.record).other_steps(self.read_changed_ticket, self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the change and outcome Attributes."""
        return dex.PersistenceSchema.of(ticket_change_attribute, ticket_change_outcome_attribute)

    # Dex Web shows no field for this binding; the analyzer requires a lone return statement.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_ticket_changed_trigger_binding(
                connection_name="gorgias-ticket-watcher", binding_name="helpdesk-ticket-events"
            )
        ]
