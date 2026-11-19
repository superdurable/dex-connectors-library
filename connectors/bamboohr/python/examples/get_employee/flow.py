# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Get one BambooHR employee through a named Dex connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.bamboohr import Connection, EmployeeRecord, GetEmployeeInput, new_get_employee_step

CONNECTION_NAME = "bamboohr"
FLOW_TYPE = "BambooHRGetEmployee"


# dex:group group-id:bamboohr group-label:"BambooHR"
# dex:explanation text:"Complete the Flow with the employee fields returned by BambooHR."
class _EmployeeFound(dex.Step[QueryResult[EmployeeRecord]]):
    def get_step_type(self) -> str:
        return "EmployeeFound"

    def execute(self, context: dex.Context, input: QueryResult[EmployeeRecord]) -> dex.StepDecision:
        return dex.graceful_complete(input.value)


class GetEmployeeFlow(dex.Flow[GetEmployeeInput]):
    """Read a selected employee and fail the Flow for unwired optional outcomes."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("BambooHR example requires its static connection name")
        self._found = _EmployeeFound()
        self._get = new_get_employee_step(
            step_type="GetBambooHREmployee",
            step_input_type=GetEmployeeInput,
            annotations={
                "groupId": "bamboohr",
                "groupLabel": "BambooHR",
                "explanation": "Read one employee and identify fields this connection cannot see.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda request: request,
            found=self._found,
        )

    def get_flow_type(self) -> str:
        """Return the stable example Flow identity."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[GetEmployeeInput]:
        """Register the provider Query and successful completion Steps."""
        return dex.StepList.start_step(self._get).other_steps(self._found)
