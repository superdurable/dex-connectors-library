# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one JD Logistics provider result in a durable Dex Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.jdlogistics import (
    Connection,
    QueryWaybillTraceInput,
    WaybillTrace,
    new_query_waybill_trace_step,
)

CONNECTION_NAME = "jdlogistics"
_RESULT = dex.Attribute("jdlogistics-waybill-trace", WaybillTrace)


def map_to_operation_input(input: QueryWaybillTraceInput) -> QueryWaybillTraceInput:
    """Pass the public Flow input to the provider Query."""
    return input


# dex:group group-id:jdlogistics group-label:"JD Logistics"
# dex:explanation text:"Start the JD Logistics Query for this Flow."
class _Start(dex.Step[QueryWaybillTraceInput]):
    def get_step_type(self) -> str:
        return "StartWaybillTraceFlow"

    def execute(self, context: dex.Context, input: QueryWaybillTraceInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("QueryWaybillTraceQuery"), input)


# dex:group group-id:jdlogistics group-label:"JD Logistics"
# dex:explanation text:"Persist the validated provider result and complete the Flow."
class _Complete(dex.Step[QueryResult[WaybillTrace]]):
    def get_step_type(self) -> str:
        return "CompleteWaybillTrace"

    def execute(self, context: dex.Context, input: QueryResult[WaybillTrace]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class WaybillTraceFlow(dex.Flow[QueryWaybillTraceInput]):
    """Call JD Logistics through the static named connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("JD Logistics connection name differs")
        self._start = _Start()
        self._complete = _Complete()
        self._query = new_query_waybill_trace_step(
            step_type="QueryWaybillTraceQuery",
            step_input_type=QueryWaybillTraceInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "JD Logistics",
                "explanation": "Read authorized JD Logistics waybill scans.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable application Flow identity."""
        return "JDLogisticsWaybillTraceFlow"

    def get_steps(self) -> dex.StepList[QueryWaybillTraceInput]:
        """Register start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable provider result Attribute."""
        return dex.PersistenceSchema.of(_RESULT)
