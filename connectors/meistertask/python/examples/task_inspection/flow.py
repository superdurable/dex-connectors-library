# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable MeisterTask task inspection Flow using getTask."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.meistertask import (
    Connection,
    GetTaskInput,
    TaskSummary,
    new_get_task_step,
)

FLOW_TYPE = "MeisterTaskTaskInspectionPython"
CONNECTION_NAME = "meistertask"
_CALL_STEP_TYPE = "CallMeisterTask"
_RESULT = dex.Attribute("meistertask-task", TaskSummary)


def map_to_operation_input(request: GetTaskInput) -> GetTaskInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:meistertask group-label:"MeisterTask"
# dex:explanation text:"Start the MeisterTask request."
class _StartRequest(dex.Step[GetTaskInput]):
    def get_step_type(self) -> str:
        return "StartMeisterTaskRequest"

    def execute(self, context: dex.Context, input: GetTaskInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallMeisterTask"), input)


# dex:group group-id:meistertask group-label:"MeisterTask"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[TaskSummary]]):
    def get_step_type(self) -> str:
        return "CompleteMeisterTaskRequest"

    def execute(self, context: dex.Context, input: QueryResult[TaskSummary]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class TaskInspectionFlow(dex.Flow[GetTaskInput]):
    """Read one task through the static meistertask connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("task inspection requires the static meistertask connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_task_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetTaskInput,
            annotations={
                "groupId": "meistertask",
                "groupLabel": "MeisterTask",
                "explanation": "Read one task from the connected MeisterTask account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "MeisterTaskTaskInspectionPython"

    def get_steps(self) -> dex.StepList[GetTaskInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
