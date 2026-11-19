# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Vidu model catalog Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.shengshu import (
    Connection,
    GetTaskStatusInput,
    GetTaskStatusOutput,
    new_get_task_status_step,
)

CONNECTION_NAME = "shengshu"
_RESULT = dex.Attribute("shengshu-provider query", GetTaskStatusOutput)


def map_to_operation_input(request: GetTaskStatusInput) -> GetTaskStatusInput:
    """Forward the provider query request."""
    return request


# dex:group group-id:shengshu group-label:"Vidu"
# dex:explanation text:"Start the Vidu provider query request."
class _StartListModels(dex.Step[GetTaskStatusInput]):
    def get_step_type(self) -> str:
        return "StartViduModelList"

    def execute(self, context: dex.Context, input: GetTaskStatusInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallVidu"), input)


# dex:group group-id:shengshu group-label:"Vidu"
# dex:explanation text:"Persist available safe provider data and complete the Flow."
class _CompleteListModels(dex.Step[QueryResult[GetTaskStatusOutput]]):
    def get_step_type(self) -> str:
        return "CompleteViduModelList"

    def execute(self, context: dex.Context, input: QueryResult[GetTaskStatusOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class GetTaskStatusFlow(dex.Flow[GetTaskStatusInput]):
    """List provider data through the static Vidu connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("GetTaskStatusFlow requires the static Vidu connection")
        self._start = _StartListModels()
        self._complete = _CompleteListModels()
        self._call = new_get_task_status_step(
            step_type="CallVidu",
            step_input_type=GetTaskStatusInput,
            annotations={
                "groupId": "shengshu",
                "groupLabel": "Vidu",
                "explanation": "List safe provider data visible to the provider API Key.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable provider query Flow identity."""
        return "SHENGSHUModelList"

    def get_steps(self) -> dex.StepList[GetTaskStatusInput]:
        """Register the provider query Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the bounded provider query result."""
        return dex.PersistenceSchema.of(_RESULT)
