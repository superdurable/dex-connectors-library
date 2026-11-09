# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Rippling worker inspection Flow using getWorker."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.rippling import (
    Connection,
    GetWorkerInput,
    WorkerSummary,
    new_get_worker_step,
)

FLOW_TYPE = "RipplingWorkerInspectionPython"
CONNECTION_NAME = "rippling"
_CALL_STEP_TYPE = "CallRippling"
_RESULT = dex.Attribute("rippling-worker", WorkerSummary)


def map_to_operation_input(request: GetWorkerInput) -> GetWorkerInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:rippling group-label:"Rippling"
# dex:explanation text:"Start the Rippling request."
class _StartRequest(dex.Step[GetWorkerInput]):
    def get_step_type(self) -> str:
        return "StartRipplingRequest"

    def execute(self, context: dex.Context, input: GetWorkerInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallRippling"), input)


# dex:group group-id:rippling group-label:"Rippling"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[WorkerSummary]]):
    def get_step_type(self) -> str:
        return "CompleteRipplingRequest"

    def execute(self, context: dex.Context, input: QueryResult[WorkerSummary]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class WorkerInspectionFlow(dex.Flow[GetWorkerInput]):
    """Read one worker through the static rippling connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("worker inspection requires the static rippling connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_worker_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetWorkerInput,
            annotations={
                "groupId": "rippling",
                "groupLabel": "Rippling",
                "explanation": "Read one worker from the connected Rippling account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "RipplingWorkerInspectionPython"

    def get_steps(self) -> dex.StepList[GetWorkerInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
