# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable FreshBooks invoice Flow using the getInvoice Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.freshbooks import (
    Connection,
    GetInvoiceInput,
    Invoice,
    new_get_invoice_step,
)

FLOW_TYPE = "FreshBooksInvoiceLookupPython"
CONNECTION_NAME = "freshbooks"
_CALL_STEP_TYPE = "CallGetInvoice"
_RESULT = dex.Attribute("freshbooks-python-invoice", Invoice)


def map_to_operation_input(request: GetInvoiceInput) -> GetInvoiceInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:freshbooks group-label:"FreshBooks"
# dex:explanation text:"Start the FreshBooks request."
class _StartRequest(dex.Step[GetInvoiceInput]):
    def get_step_type(self) -> str:
        return "StartGetInvoice"

    def execute(self, context: dex.Context, input: GetInvoiceInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallGetInvoice"), input)


# dex:group group-id:freshbooks group-label:"FreshBooks"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[Invoice]]):
    def get_step_type(self) -> str:
        return "CompleteGetInvoice"

    def execute(self, context: dex.Context, input: QueryResult[Invoice]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class InvoiceFlow(dex.Flow[GetInvoiceInput]):
    """Run one getInvoice read through the static freshbooks connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the invoice lookup Flow requires the static freshbooks connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_invoice_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetInvoiceInput,
            annotations={
                "groupId": "freshbooks",
                "groupLabel": "FreshBooks",
                "explanation": "Read the requested FreshBooks invoice.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "FreshBooksInvoiceLookupPython"

    def get_steps(self) -> dex.StepList[GetInvoiceInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
