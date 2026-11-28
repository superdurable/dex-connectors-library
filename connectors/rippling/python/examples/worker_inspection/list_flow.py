# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Rippling worker page Flow using listWorkers."""

from dataclasses import dataclass

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.rippling import (
    Connection,
    ListWorkersInput,
    WorkerPage,
    new_list_workers_step,
)

CONNECTION_NAME = "rippling"

_CALL_STEP_TYPE = "CallListWorkers"
_RESULT = dex.Attribute("rippling-python-worker-page", WorkerPage)


@dataclass(frozen=True)
class WorkerPageRequest:
    """Select one page for the Rippling query."""

    limit: int = 0
    cursor: str = ""


def map_to_operation_input(request: WorkerPageRequest) -> ListWorkersInput:
    """Build the connector's page query from the Flow input."""
    return ListWorkersInput(limit=request.limit, cursor=request.cursor)


# dex:group group-id:rippling group-label:"Rippling"
# dex:explanation text:"Start the Rippling request."
class _StartRequest(dex.Step[WorkerPageRequest]):
    def get_step_type(self) -> str:
        return "StartListWorkers"

    def execute(self, context: dex.Context, input: WorkerPageRequest) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListWorkers"), input)


# dex:group group-id:rippling group-label:"Rippling"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[WorkerPage]]):
    def get_step_type(self) -> str:
        return "CompleteListWorkers"

    def execute(self, context: dex.Context, input: QueryResult[WorkerPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class WorkerListFlow(dex.Flow[WorkerPageRequest]):
    """Run one listWorkers read through the static rippling connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the worker page Flow requires the static rippling connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_workers_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=WorkerPageRequest,
            annotations={
                "groupId": "rippling",
                "groupLabel": "Rippling",
                "explanation": "Read one page of workers visible to the connection.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "RipplingWorkerListPython"

    def get_steps(self) -> dex.StepList[WorkerPageRequest]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
