# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Gusto employee Flow using the getEmployee Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.gusto import (
    Connection,
    Employee,
    GetEmployeeInput,
    new_get_employee_step,
)

FLOW_TYPE = "GustoEmployeeLookupPython"
CONNECTION_NAME = "gusto"
_CALL_STEP_TYPE = "CallGetEmployee"
_RESULT = dex.Attribute("gusto-python-employee", Employee)


def map_to_operation_input(request: GetEmployeeInput) -> GetEmployeeInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:gusto group-label:"Gusto"
# dex:explanation text:"Start the Gusto request."
class _StartRequest(dex.Step[GetEmployeeInput]):
    def get_step_type(self) -> str:
        return "StartGetEmployee"

    def execute(self, context: dex.Context, input: GetEmployeeInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallGetEmployee"), input)


# dex:group group-id:gusto group-label:"Gusto"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[Employee]]):
    def get_step_type(self) -> str:
        return "CompleteGetEmployee"

    def execute(self, context: dex.Context, input: QueryResult[Employee]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class EmployeeFlow(dex.Flow[GetEmployeeInput]):
    """Run one getEmployee read through the static gusto connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the employee lookup Flow requires the static gusto connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_employee_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetEmployeeInput,
            annotations={
                "groupId": "gusto",
                "groupLabel": "Gusto",
                "explanation": "Read the requested Gusto employee.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "GustoEmployeeLookupPython"

    def get_steps(self) -> dex.StepList[GetEmployeeInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
