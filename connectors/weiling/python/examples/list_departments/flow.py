# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Weiling listDepartments Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.weiling import (
    Connection,
    ListDepartmentsInput,
    ListDepartmentsOutput,
    new_list_departments_step,
)

CONNECTION_NAME = "weiling"
_RESULT = dex.Attribute("weiling-result", ListDepartmentsOutput)


def map_to_operation_input(request: ListDepartmentsInput) -> ListDepartmentsInput:
    """Forward the provider request."""
    return request


# dex:group group-id:weiling group-label:"Weiling"
# dex:explanation text:"Start the Weiling query."
class _StartQuery(dex.Step[ListDepartmentsInput]):
    def execute(self, context: dex.Context, input: ListDepartmentsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:weiling group-label:"Weiling"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[ListDepartmentsOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[ListDepartmentsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListDepartmentsFlow(dex.Flow[ListDepartmentsInput]):
    """Run one Weiling query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Weiling Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_list_departments_step(
            step_type="CallProvider",
            step_input_type=ListDepartmentsInput,
            annotations={
                "groupId": "weiling",
                "groupLabel": "Weiling",
                "explanation": "Run Weiling listDepartments.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "ListDepartmentsWeiling"

    def get_steps(self) -> dex.StepList[ListDepartmentsInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
