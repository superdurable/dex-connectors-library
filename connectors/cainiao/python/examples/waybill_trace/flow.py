# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read Cainiao provider data through a durable Query Step."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.cainiao import (
    Connection,
    QueryWaybillTraceInput,
    WaybillTrace,
    new_query_waybill_trace_step,
)

CONNECTION_NAME = "cainiao"
_RESULT = dex.Attribute("cainiao-waybill-trace", WaybillTrace)


def map_to_operation_input(input: QueryWaybillTraceInput) -> QueryWaybillTraceInput:
    """Forward the nonsecret provider Query input."""
    return input


# dex:group group-id:waybill-trace group-label:"Cainiao"
# dex:explanation text:"Start the Cainiao provider Query."
class _StartQuery(dex.Step[QueryWaybillTraceInput]):
    def get_step_type(self) -> str:
        return "StartCainiaoWaybillTrace"

    def execute(self, context: dex.Context, input: QueryWaybillTraceInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("ReadCainiaoWaybillTrace"), input)


# dex:group group-id:waybill-trace group-label:"Cainiao"
# dex:explanation text:"Persist the bounded Cainiao result and complete."
class _CompleteQuery(dex.Step[QueryResult[WaybillTrace]]):
    def get_step_type(self) -> str:
        return "CompleteCainiaoWaybillTrace"

    def execute(self, context: dex.Context, input: QueryResult[WaybillTrace]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class WaybillTraceFlow(dex.Flow[QueryWaybillTraceInput]):
    """Run the Cainiao Query with the static named connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Cainiao Flow requires the static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._query = new_query_waybill_trace_step(
            step_type="ReadCainiaoWaybillTrace",
            step_input_type=QueryWaybillTraceInput,
            annotations={
                "groupId": "waybill-trace",
                "groupLabel": "Cainiao",
                "explanation": "Read the bounded provider result through Cainiao.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "CainiaoWaybillTracePython"

    def get_steps(self) -> dex.StepList[QueryWaybillTraceInput]:
        """Register the start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the bounded result Attribute."""
        return dex.PersistenceSchema.of(_RESULT)
