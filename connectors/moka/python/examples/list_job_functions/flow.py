# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Moka listJobFunctions Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.moka import (
    Connection,
    ListJobFunctionsInput,
    ListJobFunctionsOutput,
    new_list_job_functions_step,
)

CONNECTION_NAME = "moka"
_RESULT = dex.Attribute("moka-result", ListJobFunctionsOutput)


def map_to_operation_input(request: ListJobFunctionsInput) -> ListJobFunctionsInput:
    """Forward the provider request."""
    return request


# dex:group group-id:moka group-label:"Moka"
# dex:explanation text:"Start the Moka query."
class _StartQuery(dex.Step[ListJobFunctionsInput]):
    def execute(self, context: dex.Context, input: ListJobFunctionsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:moka group-label:"Moka"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[ListJobFunctionsOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[ListJobFunctionsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListJobFunctionsFlow(dex.Flow[ListJobFunctionsInput]):
    """Run one Moka query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Moka Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_list_job_functions_step(
            step_type="CallProvider",
            step_input_type=ListJobFunctionsInput,
            annotations={
                "groupId": "moka",
                "groupLabel": "Moka",
                "explanation": "Run Moka listJobFunctions.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "ListJobFunctionsMoka"

    def get_steps(self) -> dex.StepList[ListJobFunctionsInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
