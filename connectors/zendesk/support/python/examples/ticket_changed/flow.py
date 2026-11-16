# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start one Flow per Zendesk ticket created, updated, or deleted, through the ticketChanged Trigger.

The Flow records the delivered change, completes at once for a deleted ticket, and otherwise reads the
ticket back with its latest comment and completes with its current state.
"""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import QueryResult, TriggerBindingDefinition, TriggerEvent

from dex_connectors.zendesk_support import (
    TICKET_CHANGE_CREATED,
    TICKET_CHANGE_DELETED,
    TICKET_CHANGE_UPDATED,
    Connection,
    GetTicketInput,
    TicketChangedEvent,
    TicketDetails,
    define_ticket_changed_trigger_binding,
    new_get_ticket_step,
)

CONNECTION_NAME = "zendesk-ticket-watcher"
TICKET_CHANGED_TRIGGER_BINDING = "zendesk-ticket-changes"
FLOW_TYPE = "ZendeskTicketChangePython"


@dataclass(frozen=True, slots=True)
class TicketChange:
    """One ticketChanged delivery: the stable Trigger event ID beside the delivered change."""

    trigger_event_id: str = field(metadata={"dex_json_name": "triggerEventId"})
    change: TicketChangedEvent


@dataclass(frozen=True, slots=True)
class TicketChangeOutcome:
    """The Flow result: the changed ticket read back from Zendesk, or None for a deleted ticket."""

    ticket_id: int = field(metadata={"dex_json_name": "ticketId"})
    change_kind: str = field(metadata={"dex_json_name": "changeKind"})
    current: TicketDetails | None = field(default=None, metadata={"dex_json_omit_default": True})


ticket_change_attribute = dex.Attribute("zendesk-ticket-change", TicketChange)
ticket_change_outcome_attribute = dex.Attribute("zendesk-ticket-change-outcome", TicketChangeOutcome)


def resolve_ticket_change_flow_id(event: TriggerEvent[TicketChangedEvent]) -> str:
    """Name one Flow per ticket version; a redelivered change finds its existing Flow."""
    return f"zendesk-ticket-change-{event.id}"


def map_to_ticket_change(event: TriggerEvent[TicketChangedEvent]) -> TicketChange:
    """Keep the Trigger event ID beside the delivered change."""
    return TicketChange(trigger_event_id=event.id, change=event.payload)


def accepts_every_ticket_change(event: TriggerEvent[TicketChangedEvent]) -> bool:
    """Admit every change; an application narrows it, for example to one group or to new tickets."""
    return True


def map_to_get_ticket_input(change: TicketChange) -> GetTicketInput:
    """Read the changed ticket with its newest comment."""
    return GetTicketInput(ticket_id=change.change.ticket.id, latest_comment_limit=1)


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record the delivered change; complete a deletion, otherwise read the ticket back."
class RecordTicketChange(dex.Step[TicketChange]):
    """Validate and persist the delivered change."""

    def get_step_type(self) -> str:
        """Return this Step's stable type."""
        return "RecordTicketChange"

    def execute(self, context: dex.Context, input: TicketChange) -> dex.StepDecision:
        """Store the change, then complete a deletion or read the ticket."""
        if not input.trigger_event_id.strip():
            return dex.force_fail("triggerEventId is required")
        if input.change.ticket.id < 1:
            return dex.force_fail("change.ticket.id is required")
        kinds = (TICKET_CHANGE_CREATED, TICKET_CHANGE_UPDATED, TICKET_CHANGE_DELETED)
        if input.change.change_kind not in kinds:
            return dex.force_fail("change.changeKind must be created, updated, or deleted")
        ticket_change_attribute.set(context, input)
        if input.change.change_kind == TICKET_CHANGE_DELETED:
            outcome = TicketChangeOutcome(ticket_id=input.change.ticket.id, change_kind=TICKET_CHANGE_DELETED)
            ticket_change_outcome_attribute.set(context, outcome)
            return dex.graceful_complete(outcome)
        return dex.go_to(dex.StepRef("ReadChangedTicket"), input)


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record the ticket read back from Zendesk as the outcome and complete the Flow."
class CompleteTicketChange(dex.Step[QueryResult[TicketDetails]]):
    """Complete with the ticket's current state."""

    def get_step_type(self) -> str:
        """Return this Step's stable type."""
        return "CompleteTicketChange"

    def execute(self, context: dex.Context, input: QueryResult[TicketDetails]) -> dex.StepDecision:
        """Save and return the outcome."""
        change = ticket_change_attribute.get(context)
        outcome = TicketChangeOutcome(
            ticket_id=change.change.ticket.id, change_kind=change.change.change_kind, current=input.value
        )
        ticket_change_outcome_attribute.set(context, outcome)
        return dex.graceful_complete(outcome)


class TicketChangedFlow(dex.Flow[TicketChange]):
    """Record one Zendesk ticket change through the static ticket watcher connection."""

    def __init__(self, connection: Connection) -> None:
        """Bind the Zendesk Support connection at Worker registration."""
        if connection is None or connection.reference.name != CONNECTION_NAME:
            raise ValueError("ticket change Flow requires the zendesk-ticket-watcher connection")
        self.record = RecordTicketChange()
        self.complete = CompleteTicketChange()
        self.read = new_get_ticket_step(
            step_type="ReadChangedTicket",
            step_input_type=TicketChange,
            annotations={
                "groupId": "change",
                "groupLabel": "Change",
                "explanation": "Read the changed ticket back with its latest comment, so the record holds "
                "its current state rather than the export snapshot.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_get_ticket_input,
            found=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Dex Flow identity."""
        return "ZendeskTicketChangePython"

    def get_steps(self) -> dex.StepList[TicketChange]:
        """Register the record, read, and complete Steps."""
        return dex.StepList.start_step(self.record).other_steps(self.read, self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the change and outcome Attributes."""
        return dex.PersistenceSchema.of(ticket_change_attribute, ticket_change_outcome_attribute)

    # No configuration unit: Zendesk has no setup bundle, and the zero configuration delivers created and
    # updated tickets. The analyzer needs one return.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_ticket_changed_trigger_binding(
                connection_name="zendesk-ticket-watcher", binding_name="zendesk-ticket-changes"
            )
        ]
