# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start one Flow per QuickBooks invoice version, through the invoiceChanged Trigger.

The Flow records the delivered change, reads the invoice back with getInvoice, and completes with whether
it is paid.
"""

from dataclasses import dataclass, field
from decimal import Decimal

import dex
from dex_connectors.sdk import QueryResult, TriggerBindingDefinition, TriggerEvent

from dex_connectors.intuit_quickbooks import (
    INVOICE_CHANGE_CREATED,
    INVOICE_CHANGE_UPDATED,
    Connection,
    GetInvoiceInput,
    Invoice,
    InvoiceChangedEvent,
    define_invoice_changed_trigger_binding,
    new_get_invoice_step,
)

CONNECTION_NAME = "quickbooks-invoice-watcher"
INVOICE_CHANGED_TRIGGER_BINDING = "quickbooks-invoice-changes"
FLOW_TYPE = "QuickBooksInvoiceChangePython"


@dataclass(frozen=True, slots=True)
class InvoiceChange:
    """One invoiceChanged delivery: the stable Trigger event ID beside the delivered change."""

    trigger_event_id: str = field(metadata={"dex_json_name": "triggerEventId"})
    change: InvoiceChangedEvent


@dataclass(frozen=True, slots=True)
class InvoiceChangeOutcome:
    """The Flow result: the changed invoice read back from QuickBooks."""

    realm_id: str = field(metadata={"dex_json_name": "realmId"})
    invoice_id: str = field(metadata={"dex_json_name": "invoiceId"})
    change_kind: str = field(metadata={"dex_json_name": "changeKind"})
    is_paid: bool = field(metadata={"dex_json_name": "isPaid"})
    current: Invoice


invoice_change_attribute = dex.Attribute("quickbooks-invoice-change", InvoiceChange)
invoice_change_outcome_attribute = dex.Attribute("quickbooks-invoice-change-outcome", InvoiceChangeOutcome)


def resolve_invoice_change_flow_id(event: TriggerEvent[InvoiceChangedEvent]) -> str:
    """Name one Flow per invoice version; a redelivered change finds its existing Flow."""
    return f"quickbooks-invoice-change-{event.id}"


def map_to_invoice_change(event: TriggerEvent[InvoiceChangedEvent]) -> InvoiceChange:
    """Keep the Trigger event ID beside the delivered change."""
    return InvoiceChange(trigger_event_id=event.id, change=event.payload)


def accepts_every_invoice_change(event: TriggerEvent[InvoiceChangedEvent]) -> bool:
    """Admit every change; an application narrows it, for example to one customer."""
    return True


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record the delivered invoice change before reading the invoice back."
class RecordInvoiceChange(dex.Step[InvoiceChange]):
    """Validate and persist the delivered change."""

    def get_step_type(self) -> str:
        """Return this Step's stable type."""
        return "RecordInvoiceChange"

    def execute(self, context: dex.Context, input: InvoiceChange) -> dex.StepDecision:
        """Store the change and read the invoice."""
        if not input.trigger_event_id.strip():
            return dex.force_fail("triggerEventId is required")
        if not input.change.realm_id or not input.change.invoice.invoice_id:
            return dex.force_fail("change.realmId and change.invoice.invoiceId are required")
        if input.change.change_kind not in (INVOICE_CHANGE_CREATED, INVOICE_CHANGE_UPDATED):
            return dex.force_fail("change.changeKind must be created or updated")
        invoice_change_attribute.set(context, input)
        return dex.go_to(dex.StepRef("ReadChangedInvoice"), input)


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record whether the invoice read back from QuickBooks is paid and complete the Flow."
class CompleteInvoiceChange(dex.Step[QueryResult[Invoice]]):
    """Complete with the invoice's current state."""

    def get_step_type(self) -> str:
        """Return this Step's stable type."""
        return "CompleteInvoiceChange"

    def execute(self, context: dex.Context, input: QueryResult[Invoice]) -> dex.StepDecision:
        """Save and return the outcome."""
        change = invoice_change_attribute.get(context)
        outcome = InvoiceChangeOutcome(
            realm_id=change.change.realm_id,
            invoice_id=change.change.invoice.invoice_id,
            change_kind=change.change.change_kind,
            is_paid=Decimal(input.value.balance or "0") == 0,
            current=input.value,
        )
        invoice_change_outcome_attribute.set(context, outcome)
        return dex.graceful_complete(outcome)


class InvoiceChangedFlow(dex.Flow[InvoiceChange]):
    """Record one QuickBooks invoice change through the static invoice watcher connection."""

    def __init__(self, connection: Connection) -> None:
        """Bind the QuickBooks connection at Worker registration."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("invoice change Flow requires the quickbooks-invoice-watcher connection")
        self.record = RecordInvoiceChange()
        self.complete = CompleteInvoiceChange()
        self.read = new_get_invoice_step(
            step_type="ReadChangedInvoice",
            step_input_type=InvoiceChange,
            annotations={
                "groupId": "change",
                "groupLabel": "Change",
                "explanation": "Read the changed invoice back, so the record holds its current balance "
                "rather than the polled copy.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda change: GetInvoiceInput(change.change.invoice.invoice_id),
            found=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Dex Flow identity."""
        return "QuickBooksInvoiceChangePython"

    def get_steps(self) -> dex.StepList[InvoiceChange]:
        """Register the record, read, and complete Steps."""
        return dex.StepList.start_step(self.record).other_steps(self.read, self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the change and outcome Attributes."""
        return dex.PersistenceSchema.of(invoice_change_attribute, invoice_change_outcome_attribute)

    # No picker units: the binding watches the whole company and has no fields. The analyzer needs one return.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_invoice_changed_trigger_binding(
                connection_name="quickbooks-invoice-watcher", binding_name="quickbooks-invoice-changes"
            )
        ]
