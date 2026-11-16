# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start one Flow per Xero sales invoice or bill created or changed, through the invoiceChanged Trigger.

The Flow records the delivered change, reads the invoice back with getInvoice, and completes with the
invoice's current state.
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

from dex_connectors.xero import (
    Connection,
    GetInvoiceInput,
    Invoice,
    InvoiceChangedEvent,
    define_invoice_changed_trigger_binding,
    new_get_invoice_step,
)

CONNECTION_NAME = "xero-invoice-watcher"
INVOICE_CHANGED_TRIGGER_BINDING = "invoice-changes"
FLOW_TYPE = "XeroInvoiceChangePython"


@dataclass(frozen=True, slots=True)
class InvoiceChange:
    """One invoiceChanged delivery: the stable Trigger event ID beside the delivered change."""

    trigger_event_id: str = field(metadata={"dex_json_name": "triggerEventId"})
    change: InvoiceChangedEvent


@dataclass(frozen=True, slots=True)
class InvoiceChangeOutcome:
    """The Flow result: the changed invoice read back from Xero."""

    invoice_id: str = field(metadata={"dex_json_name": "invoiceId"})
    delivered_status: str = field(metadata={"dex_json_name": "deliveredStatus"})
    current: Invoice


invoice_change_attribute = dex.Attribute("xero-invoice-change", InvoiceChange)
invoice_change_outcome_attribute = dex.Attribute("xero-invoice-change-outcome", InvoiceChangeOutcome)


def resolve_invoice_change_flow_id(event: TriggerEvent[InvoiceChangedEvent]) -> str:
    """Name one Flow per change; a redelivered change finds its existing Flow."""
    return f"xero-invoice-change-{event.id}"


def map_to_invoice_change(event: TriggerEvent[InvoiceChangedEvent]) -> InvoiceChange:
    """Keep the Trigger event ID beside the delivered change."""
    return InvoiceChange(trigger_event_id=event.id, change=event.payload)


def accepts_every_invoice_change(event: TriggerEvent[InvoiceChangedEvent]) -> bool:
    """Admit every change; the binding's filter already narrows type and status."""
    return True


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record the delivered invoice change before reading the invoice back from Xero."
class RecordInvoiceChange(dex.Step[InvoiceChange]):
    """Validate and persist the delivered change."""

    def get_step_type(self) -> str:
        """Return this Step's stable type."""
        return "RecordInvoiceChange"

    def execute(self, context: dex.Context, input: InvoiceChange) -> dex.StepDecision:
        """Store the change and read the invoice."""
        if not input.trigger_event_id.strip():
            return dex.force_fail("triggerEventId is required")
        if not input.change.invoice.invoice_id.strip():
            return dex.force_fail("change.invoice.invoiceId is required")
        if not input.change.invoice.status:
            return dex.force_fail("change.invoice.status is required")
        invoice_change_attribute.set(context, input)
        return dex.go_to(dex.StepRef("ReadChangedInvoice"), input)


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record the invoice read back from Xero as the outcome and complete the Flow."
class CompleteInvoiceChange(dex.Step[QueryResult[Invoice]]):
    """Complete with the invoice's current state."""

    def get_step_type(self) -> str:
        """Return this Step's stable type."""
        return "CompleteInvoiceChange"

    def execute(self, context: dex.Context, input: QueryResult[Invoice]) -> dex.StepDecision:
        """Save and return the outcome."""
        change = invoice_change_attribute.get(context)
        outcome = InvoiceChangeOutcome(
            invoice_id=change.change.invoice.invoice_id,
            delivered_status=change.change.invoice.status,
            current=input.value,
        )
        invoice_change_outcome_attribute.set(context, outcome)
        return dex.graceful_complete(outcome)


class InvoiceChangedFlow(dex.Flow[InvoiceChange]):
    """Record one Xero invoice change through the static invoice watcher connection."""

    def __init__(self, connection: Connection) -> None:
        """Bind the Xero connection at Worker registration."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("invoice change Flow requires the xero-invoice-watcher connection")
        self.record = RecordInvoiceChange()
        self.complete = CompleteInvoiceChange()
        self.read = new_get_invoice_step(
            step_type="ReadChangedInvoice",
            step_input_type=InvoiceChange,
            annotations={
                "groupId": "change",
                "groupLabel": "Change",
                "explanation": "Read the changed invoice back, so the record holds its current state "
                "rather than the polled copy.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda change: GetInvoiceInput(change.change.invoice.invoice_id),
            found=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Dex Flow identity."""
        return "XeroInvoiceChangePython"

    def get_steps(self) -> dex.StepList[InvoiceChange]:
        """Register the record, read, and complete Steps."""
        return dex.StepList.start_step(self.record).other_steps(self.read, self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the change and outcome Attributes."""
        return dex.PersistenceSchema.of(invoice_change_attribute, invoice_change_outcome_attribute)

    # Declares the invoiceChanged binding and its invoice filter for Dex Web. The Dex analyzer requires the
    # method body to be one return statement, so it has no docstring.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_invoice_changed_trigger_binding(
                connection_name="xero-invoice-watcher",
                binding_name="invoice-changes",
                configuration_ui=ConnectorConfigurationUI(
                    units=(
                        ConnectorUIUnit(
                            id="invoiceFilter",
                            unit_id="invoiceChangeFilter",
                            label="Invoices that start this Flow",
                            description="Choose Sales invoices (ACCREC), Bills (ACCPAY), or both, and "
                            "optionally the statuses an invoice must have after its change, such as PAID to "
                            "react only to payments in full; Xero's own values are saved. Leave it unsaved "
                            "to start a Flow for every invoice and bill created or changed in the "
                            "organisation. The first start records existing invoices as a baseline and "
                            "starts no Flow for them. Restart the Worker after saving; a changed filter "
                            "records a new baseline.",
                            required=False,
                            bindings=(
                                ConnectorUIBinding(port="type", json_pointer="/type"),
                                ConnectorUIBinding(port="statuses", json_pointer="/statuses"),
                            ),
                        ),
                    )
                ),
            )
        ]
