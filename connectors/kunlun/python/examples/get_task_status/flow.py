# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read kunlun data through the static Dex connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.kunlun import (
    Connection,
    GetTaskStatusInput,
    GetTaskStatusOutput,
    new_get_task_status_step,
)

CONNECTION_NAME = "kunlun"
_OUTCOME = dex.Attribute("kunlun-get_task_status-result", GetTaskStatusOutput)


def map_to_operation_input(input: GetTaskStatusInput) -> GetTaskStatusInput:
    """Pass the typed request to the provider Query."""
    return input


# dex:group group-id:get-task-status group-label:"GetTaskStatus"
# dex:explanation text:"Start the kunlun Query."
class _StartRequest(dex.Step[GetTaskStatusInput]):
    def get_step_type(self) -> str:
        return "StartGetTaskStatus"

    def execute(self, context: dex.Context, input: GetTaskStatusInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallGetTaskStatus"), input)


# dex:group group-id:get-task-status group-label:"GetTaskStatus"
# dex:explanation text:"Persist the provider result and complete the Flow."
class _CompleteResult(dex.Step[QueryResult[GetTaskStatusOutput]]):
    def get_step_type(self) -> str:
        return "CompleteGetTaskStatus"

    def execute(self, context: dex.Context, input: QueryResult[GetTaskStatusOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class GetTaskStatusFlow(dex.Flow[GetTaskStatusInput]):
    """Run getTaskStatus using the static `kunlun` connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("kunlun example requires the static connection")
        self._start = _StartRequest()
        self._complete = _CompleteResult()
        self._query = new_get_task_status_step(
            step_type="CallGetTaskStatus",
            step_input_type=GetTaskStatusInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "kunlun",
                "explanation": "Call the kunlun getTaskStatus Query.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "KunlunGetTaskStatus"

    def get_steps(self) -> dex.StepList[GetTaskStatusInput]:
        """Register the start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the typed provider output."""
        return dex.PersistenceSchema.of(_OUTCOME)
