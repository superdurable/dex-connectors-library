# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read a QuickBooks invoice through a Dex Query Step."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.intuit_quickbooks import (
    Connection,
    GetInvoiceInput,
    Invoice,
    new_get_invoice_step,
)

CONNECTION_NAME = "quickbooks-invoice-reader"


@dataclass(frozen=True, slots=True)
class InvoiceLookupInput:
    """Invoice ID requested by the application user."""

    invoice_id: str = field(metadata={"dex_json_name": "invoiceId"})

    def to_operation_input(self) -> GetInvoiceInput:
        """Map Flow input to the QuickBooks operation input."""
        return GetInvoiceInput(self.invoice_id)


invoice_result = dex.Attribute("quickbooks-invoice", Invoice)


# dex:group group-id:quickbooks-invoice group-label:"QuickBooks Invoice"
# dex:explanation text:"Persist the QuickBooks invoice and complete the Flow."
class CompleteInvoice(dex.Step[QueryResult[Invoice]]):
    """Complete after a successful invoice read."""

    def execute(self, context: dex.Context, input: QueryResult[Invoice]) -> dex.StepDecision:
        """Persist only public invoice data and return it to the caller."""
        invoice_result.set(context, input.value)
        return dex.graceful_complete(input.value)


class InvoiceLookupFlow(dex.Flow[InvoiceLookupInput]):
    """Look up an invoice through the static QuickBooks connection."""

    def __init__(self, connection: Connection) -> None:
        self.complete = CompleteInvoice()
        self.get_invoice = new_get_invoice_step(
            step_type="GetQuickBooksInvoice",
            step_input_type=InvoiceLookupInput,
            annotations={
                "groupId": "quickbooks-invoice",
                "groupLabel": "QuickBooks Invoice",
                "explanation": "Read one QuickBooks invoice by its ID.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=InvoiceLookupInput.to_operation_input,
            found=self.complete,
        )

    def get_flow_type(self) -> str:
        """Keep the example Flow identity stable."""
        return "QuickBooksGetInvoicePython"

    def get_steps(self) -> dex.StepList[InvoiceLookupInput]:
        """Start with the connector Query and finish on found."""
        return dex.StepList.start_step(self.get_invoice).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the invoice Attribute consumed by this Flow."""
        return dex.PersistenceSchema.of(invoice_result)
