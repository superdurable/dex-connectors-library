# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Sage Intacct invoice Flow using the getInvoice Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.sage_intacct import (
    Connection,
    GetInvoiceInput,
    InvoiceSummary,
    new_get_invoice_step,
)

FLOW_TYPE = "SageIntacctInvoiceInspectionPython"
CONNECTION_NAME = "sage-intacct"
_CALL_STEP_TYPE = "CallGetInvoice"
_RESULT = dex.Attribute("sage-intacct-python-invoice", InvoiceSummary)


def map_to_operation_input(request: GetInvoiceInput) -> GetInvoiceInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:sageintacct group-label:"Sage Intacct"
# dex:explanation text:"Start the Sage Intacct request."
class _StartRequest(dex.Step[GetInvoiceInput]):
    def get_step_type(self) -> str:
        return "StartGetInvoice"

    def execute(self, context: dex.Context, input: GetInvoiceInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallGetInvoice"), input)


# dex:group group-id:sageintacct group-label:"Sage Intacct"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[InvoiceSummary]]):
    def get_step_type(self) -> str:
        return "CompleteGetInvoice"

    def execute(self, context: dex.Context, input: QueryResult[InvoiceSummary]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class InvoiceFlow(dex.Flow[GetInvoiceInput]):
    """Run one getInvoice read through the static Sage Intacct connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the invoice lookup Flow requires the static sage-intacct connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_invoice_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetInvoiceInput,
            annotations={
                "groupId": "sage-intacct",
                "groupLabel": "Sage Intacct",
                "explanation": "Read the selected Sage Intacct invoice.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "SageIntacctInvoiceInspectionPython"

    def get_steps(self) -> dex.StepList[GetInvoiceInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
