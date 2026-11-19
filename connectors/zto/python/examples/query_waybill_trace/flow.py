# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable ZTO queryWaybillTrace Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.zto import (
    Connection,
    QueryWaybillTraceInput,
    WaybillTrace,
    new_query_waybill_trace_step,
)

CONNECTION_NAME = "zto"
_RESULT = dex.Attribute("zto-result", WaybillTrace)


def map_to_operation_input(request: QueryWaybillTraceInput) -> QueryWaybillTraceInput:
    """Forward the provider request."""
    return request


# dex:group group-id:zto group-label:"ZTO"
# dex:explanation text:"Start the ZTO query."
class _StartQuery(dex.Step[QueryWaybillTraceInput]):
    def execute(self, context: dex.Context, input: QueryWaybillTraceInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:zto group-label:"ZTO"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[WaybillTrace]]):
    def execute(self, context: dex.Context, input: QueryResult[WaybillTrace]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class QueryWaybillTraceFlow(dex.Flow[QueryWaybillTraceInput]):
    """Run one ZTO query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("ZTO Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_query_waybill_trace_step(
            step_type="CallProvider",
            step_input_type=QueryWaybillTraceInput,
            annotations={
                "groupId": "zto",
                "groupLabel": "ZTO",
                "explanation": "Run ZTO queryWaybillTrace.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "QueryWaybillTraceZTO"

    def get_steps(self) -> dex.StepList[QueryWaybillTraceInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
