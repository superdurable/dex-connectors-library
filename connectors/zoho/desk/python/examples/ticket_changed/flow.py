# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start one Flow per Zoho Desk ticket created or changed in the watched department.

The Flow records the delivered change, reads the ticket back with its newest threads and comments, which
the search page leaves out, and completes with the ticket's current state.
"""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import (
    ConnectorConfigurationUI,
    ConnectorUIBinding,
    ConnectorUIUnit,
    QueryResult,
    TriggerBindingDefinition,
    TriggerEvent,
)

from dex_connectors.zoho_desk import (
    TICKET_CHANGE_CREATED,
    TICKET_CHANGE_UPDATED,
    Connection,
    GetTicketInput,
    TicketChangedEvent,
    TicketDetails,
    define_ticket_changed_trigger_binding,
    new_get_ticket_step,
)

CONNECTION_NAME = "zoho-desk-ticket-watcher"
TICKET_CHANGED_TRIGGER_BINDING = "desk-ticket-changes"
FLOW_TYPE = "ZohoDeskTicketChangePython"


@dataclass(frozen=True, slots=True)
class TicketChange:
    """One ticketChanged delivery: the stable Trigger event ID beside the delivered change."""

    trigger_event_id: str = field(metadata={"dex_json_name": "triggerEventId"})
    change: TicketChangedEvent


@dataclass(frozen=True, slots=True)
class TicketChangeOutcome:
    """The Flow result: the changed ticket read back from Zoho Desk."""

    ticket_id: str = field(metadata={"dex_json_name": "ticketId"})
    change_kind: str = field(metadata={"dex_json_name": "changeKind"})
    current: TicketDetails


ticket_change_attribute = dex.Attribute("zoho-desk-ticket-change", TicketChange)
ticket_change_outcome_attribute = dex.Attribute("zoho-desk-ticket-change-outcome", TicketChangeOutcome)


def resolve_ticket_change_flow_id(event: TriggerEvent[TicketChangedEvent]) -> str:
    """Name one Flow per ticket version; a redelivered change finds its existing Flow."""
    return f"zoho-desk-ticket-change-{event.id}"


def map_to_ticket_change(event: TriggerEvent[TicketChangedEvent]) -> TicketChange:
    """Keep the Trigger event ID beside the delivered change."""
    return TicketChange(trigger_event_id=event.id, change=event.payload)


def accepts_every_ticket_change(event: TriggerEvent[TicketChangedEvent]) -> bool:
    """Admit every change; an application narrows it, for example to new tickets."""
    return True


def map_to_get_ticket_input(change: TicketChange) -> GetTicketInput:
    """Read the changed ticket with the default thread and comment limits."""
    return GetTicketInput(ticket_id=change.change.ticket.id)


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Validate and record the delivered ticket change, then read the ticket back."
class RecordTicketChange(dex.Step[TicketChange]):
    """Validate and persist the delivered change."""

    def get_step_type(self) -> str:
        """Return this Step's stable type."""
        return "RecordTicketChange"

    def execute(self, context: dex.Context, input: TicketChange) -> dex.StepDecision:
        """Store the change and read the ticket."""
        if not input.trigger_event_id.strip():
            return dex.force_fail("triggerEventId is required")
        if not input.change.org_id.strip() or not input.change.ticket.id.strip():
            return dex.force_fail("change.orgId and change.ticket.id are required")
        if input.change.change_kind not in (TICKET_CHANGE_CREATED, TICKET_CHANGE_UPDATED):
            return dex.force_fail("change.changeKind must be created or updated")
        ticket_change_attribute.set(context, input)
        return dex.go_to(dex.StepRef("ReadChangedTicket"), input)


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record the ticket read back from Zoho Desk as the outcome and complete the Flow."
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
    """Record one Zoho Desk ticket change through the static ticket watcher connection."""

    def __init__(self, connection: Connection) -> None:
        """Bind the Zoho Desk connection at Worker registration."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("ticket change Flow requires the zoho-desk-ticket-watcher connection")
        self.record = RecordTicketChange()
        self.complete = CompleteTicketChange()
        self.read = new_get_ticket_step(
            step_type="ReadChangedTicket",
            step_input_type=TicketChange,
            annotations={
                "groupId": "change",
                "groupLabel": "Change",
                "explanation": "Read the changed ticket back with its newest threads and comments, which the "
                "search page leaves out.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_get_ticket_input,
            found=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Dex Flow identity."""
        return "ZohoDeskTicketChangePython"

    def get_steps(self) -> dex.StepList[TicketChange]:
        """Register the record, read, and complete Steps."""
        return dex.StepList.start_step(self.record).other_steps(self.read, self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the change and outcome Attributes."""
        return dex.PersistenceSchema.of(ticket_change_attribute, ticket_change_outcome_attribute)

    # Declares the ticketChanged binding and its department picker for Dex Web; Dex reads one static return.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_ticket_changed_trigger_binding(
                connection_name="zoho-desk-ticket-watcher",
                binding_name="desk-ticket-changes",
                configuration_ui=ConnectorConfigurationUI(
                    (
                        ConnectorUIUnit(
                            "watchedDepartment",
                            "departmentPicker",
                            "Watched department",
                            "Choose the Zoho Desk department whose created and changed tickets start "
                            "this Flow; the list comes from the connection's organization and saves the "
                            "department ID and name. Leave it blank to watch every department the "
                            "connection's agent can see. The first start records recent tickets as a "
                            "baseline and starts no Flow for them. Restart the Worker after saving; a "
                            "changed department records a new baseline.",
                            False,
                            (
                                ConnectorUIBinding("departmentId", "/departmentId"),
                                ConnectorUIBinding("departmentName", "/departmentName"),
                            ),
                        ),
                    )
                ),
            )
        ]
