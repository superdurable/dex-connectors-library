# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Oracle NetSuite customer Flow using the getCustomer Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.oracle_netsuite import (
    Connection,
    CustomerSummary,
    GetCustomerInput,
    new_get_customer_step,
)

FLOW_TYPE = "OracleNetSuiteCustomerInspectionPython"
CONNECTION_NAME = "oracle-netsuite"
_CALL_STEP_TYPE = "CallGetCustomer"
_RESULT = dex.Attribute("oracle-netsuite-python-customer", CustomerSummary)


def map_to_operation_input(request: GetCustomerInput) -> GetCustomerInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:oracle-netsuite group-label:"Oracle NetSuite"
# dex:explanation text:"Start the Oracle NetSuite request."
class _StartRequest(dex.Step[GetCustomerInput]):
    def get_step_type(self) -> str:
        return "StartGetCustomer"

    def execute(self, context: dex.Context, input: GetCustomerInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallGetCustomer"), input)


# dex:group group-id:oracle-netsuite group-label:"Oracle NetSuite"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[CustomerSummary]]):
    def get_step_type(self) -> str:
        return "CompleteGetCustomer"

    def execute(self, context: dex.Context, input: QueryResult[CustomerSummary]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class CustomerFlow(dex.Flow[GetCustomerInput]):
    """Run one getCustomer read through the static Oracle NetSuite connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the customer lookup Flow requires the static oracle-netsuite connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_customer_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetCustomerInput,
            annotations={
                "groupId": "oracle-netsuite",
                "groupLabel": "Oracle NetSuite",
                "explanation": "Read the selected Oracle NetSuite customer.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "OracleNetSuiteCustomerInspectionPython"

    def get_steps(self) -> dex.StepList[GetCustomerInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
