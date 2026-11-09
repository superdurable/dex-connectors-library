# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Customerio customer lookup Flow using getCustomersByEmail."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.customerio import (
    Connection,
    CustomerLookupPage,
    GetCustomersByEmailInput,
    new_get_customers_by_email_step,
)

FLOW_TYPE = "CustomerioCustomerLookupPython"
CONNECTION_NAME = "customerio"
_CALL_STEP_TYPE = "CallCustomerio"
_RESULT = dex.Attribute("customerio-customer-lookup", CustomerLookupPage)


def map_to_operation_input(request: GetCustomersByEmailInput) -> GetCustomersByEmailInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:customerio group-label:"Customer.io"
# dex:explanation text:"Start the Customerio request."
class _StartRequest(dex.Step[GetCustomersByEmailInput]):
    def get_step_type(self) -> str:
        return "StartCustomerioRequest"

    def execute(self, context: dex.Context, input: GetCustomersByEmailInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallCustomerio"), input)


# dex:group group-id:customerio group-label:"Customer.io"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[CustomerLookupPage]]):
    def get_step_type(self) -> str:
        return "CompleteCustomerioRequest"

    def execute(self, context: dex.Context, input: QueryResult[CustomerLookupPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class CustomerLookupFlow(dex.Flow[GetCustomersByEmailInput]):
    """Read customer IDs through the static customerio connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("customer lookup requires the static customerio connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_customers_by_email_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetCustomersByEmailInput,
            annotations={
                "groupId": "customerio",
                "groupLabel": "Customer.io",
                "explanation": "Read customer IDs from the connected Customer.io workspace.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "CustomerioCustomerLookupPython"

    def get_steps(self) -> dex.StepList[GetCustomersByEmailInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
