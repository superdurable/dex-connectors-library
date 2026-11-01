# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Coda sample Flow using the listRows Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.coda import Connection, ListRowsInput, ListRowsOutput, new_list_rows_step

FLOW_TYPE = "CodaSample"
CONNECTION_NAME = "coda"
_CALL_STEP_TYPE = "CallCoda"
_RESULT = dex.Attribute("coda-sample-result", ListRowsOutput)


def map_to_operation_input(request: ListRowsInput) -> ListRowsInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:coda group-label:"Coda"
# dex:explanation text:"Start the Coda request."
class _StartRequest(dex.Step[ListRowsInput]):
    def get_step_type(self) -> str:
        return "StartCodaRequest"

    def execute(self, context: dex.Context, input: ListRowsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef(_CALL_STEP_TYPE), input)


# dex:group group-id:coda group-label:"Coda"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[ListRowsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteCodaRequest"

    def execute(self, context: dex.Context, input: QueryResult[ListRowsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class SampleFlow(dex.Flow[ListRowsInput]):
    """Run one listRows read through the static coda connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the sample requires the static coda connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_rows_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListRowsInput,
            annotations={
                "groupId": "coda",
                "groupLabel": "Coda",
                "explanation": "List one page of rows in a Coda table.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[ListRowsInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
