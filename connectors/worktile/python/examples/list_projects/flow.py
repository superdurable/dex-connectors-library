# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Worktile listProjects Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.worktile import (
    Connection,
    ListProjectsInput,
    ListProjectsOutput,
    new_list_projects_step,
)

CONNECTION_NAME = "worktile"
_RESULT = dex.Attribute("worktile-result", ListProjectsOutput)


def map_to_operation_input(request: ListProjectsInput) -> ListProjectsInput:
    """Forward the provider request."""
    return request


# dex:group group-id:worktile group-label:"Worktile"
# dex:explanation text:"Start the Worktile query."
class _StartQuery(dex.Step[ListProjectsInput]):
    def execute(self, context: dex.Context, input: ListProjectsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:worktile group-label:"Worktile"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[ListProjectsOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[ListProjectsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListProjectsFlow(dex.Flow[ListProjectsInput]):
    """Run one Worktile query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Worktile Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_list_projects_step(
            step_type="CallProvider",
            step_input_type=ListProjectsInput,
            annotations={
                "groupId": "worktile",
                "groupLabel": "Worktile",
                "explanation": "Run Worktile listProjects.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "ListProjectsWorktile"

    def get_steps(self) -> dex.StepList[ListProjectsInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
