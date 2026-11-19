# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Hesi department directory Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.hesi import (
    Connection,
    ListDepartmentsInput,
    ListDepartmentsOutput,
    new_list_departments_step,
)

CONNECTION_NAME = "hesi"
_RESULT = dex.Attribute("hesi-department-directory", ListDepartmentsOutput)


def map_to_operation_input(request: ListDepartmentsInput) -> ListDepartmentsInput:
    """Forward the requested department page to Hesi."""
    return request


# dex:group group-id:hesi group-label:"Hesi"
# dex:explanation text:"Start the Hesi department directory request."
class _StartDepartments(dex.Step[ListDepartmentsInput]):
    def get_step_type(self) -> str:
        return "StartHesiDepartments"

    def execute(self, context: dex.Context, input: ListDepartmentsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallHesi"), input)


# dex:group group-id:hesi group-label:"Hesi"
# dex:explanation text:"Persist the department page and complete the Flow."
class _CompleteDepartments(dex.Step[QueryResult[ListDepartmentsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteHesiDepartments"

    def execute(self, context: dex.Context, input: QueryResult[ListDepartmentsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class DepartmentDirectoryFlow(dex.Flow[ListDepartmentsInput]):
    """Read one department page through the static Hesi connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("DepartmentDirectoryFlow requires the static Hesi connection")
        self._start = _StartDepartments()
        self._complete = _CompleteDepartments()
        self._call = new_list_departments_step(
            step_type="CallHesi",
            step_input_type=ListDepartmentsInput,
            annotations={
                "groupId": "hesi",
                "groupLabel": "Hesi",
                "explanation": "Read one bounded page of department metadata.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable department directory Flow identity."""
        return "HesiDepartmentDirectory"

    def get_steps(self) -> dex.StepList[ListDepartmentsInput]:
        """Register the department directory Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the department page."""
        return dex.PersistenceSchema.of(_RESULT)
