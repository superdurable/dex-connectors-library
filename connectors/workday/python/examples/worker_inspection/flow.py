# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Workday worker Flow using the getWorker Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.workday import (
    Connection,
    GetWorkerInput,
    WorkerSummary,
    new_get_worker_step,
)

FLOW_TYPE = "WorkdayWorkerInspectionPython"
CONNECTION_NAME = "workday"
_CALL_STEP_TYPE = "CallGetWorker"
_RESULT = dex.Attribute("workday-python-worker", WorkerSummary)


def map_to_operation_input(request: GetWorkerInput) -> GetWorkerInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:workday group-label:"Workday"
# dex:explanation text:"Start the Workday request."
class _StartRequest(dex.Step[GetWorkerInput]):
    def get_step_type(self) -> str:
        return "StartGetWorker"

    def execute(self, context: dex.Context, input: GetWorkerInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallGetWorker"), input)


# dex:group group-id:workday group-label:"Workday"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[WorkerSummary]]):
    def get_step_type(self) -> str:
        return "CompleteGetWorker"

    def execute(self, context: dex.Context, input: QueryResult[WorkerSummary]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class WorkerFlow(dex.Flow[GetWorkerInput]):
    """Run one getWorker read through the static Workday connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the worker lookup Flow requires the static workday connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_worker_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetWorkerInput,
            annotations={
                "groupId": "workday",
                "groupLabel": "Workday",
                "explanation": "Read the selected Workday worker.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "WorkdayWorkerInspectionPython"

    def get_steps(self) -> dex.StepList[GetWorkerInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
