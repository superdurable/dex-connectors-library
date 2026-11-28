# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Gusto company employee page Flow using listEmployees."""

from dataclasses import dataclass

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.gusto import (
    Connection,
    EmployeesPage,
    ListEmployeesInput,
    new_list_employees_step,
)

CONNECTION_NAME = "gusto"

_CALL_STEP_TYPE = "CallListEmployees"
_RESULT = dex.Attribute("gusto-python-employee-page", EmployeesPage)


@dataclass(frozen=True)
class EmployeePageRequest:
    """Select one page for the Gusto query."""

    company_id: str
    page: int = 1
    per_page: int = 25


def map_to_operation_input(request: EmployeePageRequest) -> ListEmployeesInput:
    """Build the connector's page query from the Flow input."""
    return ListEmployeesInput(company_id=request.company_id, page=request.page, per_page=request.per_page)


# dex:group group-id:gusto group-label:"Gusto"
# dex:explanation text:"Start the Gusto request."
class _StartRequest(dex.Step[EmployeePageRequest]):
    def get_step_type(self) -> str:
        return "StartListEmployees"

    def execute(self, context: dex.Context, input: EmployeePageRequest) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListEmployees"), input)


# dex:group group-id:gusto group-label:"Gusto"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[EmployeesPage]]):
    def get_step_type(self) -> str:
        return "CompleteListEmployees"

    def execute(self, context: dex.Context, input: QueryResult[EmployeesPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class EmployeeListFlow(dex.Flow[EmployeePageRequest]):
    """Run one listEmployees read through the static gusto connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the employee page Flow requires the static gusto connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_employees_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=EmployeePageRequest,
            annotations={
                "groupId": "gusto",
                "groupLabel": "Gusto",
                "explanation": "Read one page of employees from the requested company.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "GustoEmployeeListPython"

    def get_steps(self) -> dex.StepList[EmployeePageRequest]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
