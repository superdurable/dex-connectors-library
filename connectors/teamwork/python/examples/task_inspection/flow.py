# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Teamwork task inspection Flow using getTask."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.teamwork import (
    Connection,
    GetTaskInput,
    TaskSummary,
    new_get_task_step,
)

FLOW_TYPE = "TeamworkTaskInspectionPython"
CONNECTION_NAME = "teamwork"
_CALL_STEP_TYPE = "CallTeamwork"
_RESULT = dex.Attribute("teamwork-task", TaskSummary)


def map_to_operation_input(request: GetTaskInput) -> GetTaskInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:teamwork group-label:"Teamwork"
# dex:explanation text:"Start the Teamwork request."
class _StartRequest(dex.Step[GetTaskInput]):
    def get_step_type(self) -> str:
        return "StartTeamworkRequest"

    def execute(self, context: dex.Context, input: GetTaskInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallTeamwork"), input)


# dex:group group-id:teamwork group-label:"Teamwork"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[TaskSummary]]):
    def get_step_type(self) -> str:
        return "CompleteTeamworkRequest"

    def execute(self, context: dex.Context, input: QueryResult[TaskSummary]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class TaskInspectionFlow(dex.Flow[GetTaskInput]):
    """Read one task through the static teamwork connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("task inspection requires the static teamwork connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_task_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetTaskInput,
            annotations={
                "groupId": "teamwork",
                "groupLabel": "Teamwork",
                "explanation": "Read one task from the connected Teamwork account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "TeamworkTaskInspectionPython"

    def get_steps(self) -> dex.StepList[GetTaskInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
