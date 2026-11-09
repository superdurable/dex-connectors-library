# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Wrike task inspection Flow using getTask."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.wrike import (
    Connection,
    GetTaskInput,
    TaskStatus,
    new_get_task_step,
)

FLOW_TYPE = "WrikeTaskInspectionPython"
CONNECTION_NAME = "wrike"
_CALL_STEP_TYPE = "CallWrike"
_RESULT = dex.Attribute("wrike-task", TaskStatus)


def map_to_operation_input(request: GetTaskInput) -> GetTaskInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:wrike group-label:"Wrike"
# dex:explanation text:"Start the Wrike request."
class _StartRequest(dex.Step[GetTaskInput]):
    def get_step_type(self) -> str:
        return "StartWrikeRequest"

    def execute(self, context: dex.Context, input: GetTaskInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallWrike"), input)


# dex:group group-id:wrike group-label:"Wrike"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[TaskStatus]]):
    def get_step_type(self) -> str:
        return "CompleteWrikeRequest"

    def execute(self, context: dex.Context, input: QueryResult[TaskStatus]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class TaskInspectionFlow(dex.Flow[GetTaskInput]):
    """Read one task through the static wrike connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("task inspection requires the static wrike connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_task_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetTaskInput,
            annotations={
                "groupId": "wrike",
                "groupLabel": "Wrike",
                "explanation": "Read one task from the connected Wrike account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "WrikeTaskInspectionPython"

    def get_steps(self) -> dex.StepList[GetTaskInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
