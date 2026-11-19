# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable SF Express route-history Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.sfexpress import (
    Connection,
    QueryRoutesInput,
    RouteQueryResult,
    new_query_routes_step,
)

CONNECTION_NAME = "sfexpress"
_RESULT = dex.Attribute("sfexpress-route-history", RouteQueryResult)


def map_to_operation_input(request: QueryRoutesInput) -> QueryRoutesInput:
    """Forward the waybill route ID to SF Express."""
    return request


# dex:group group-id:sfexpress group-label:"SF Express"
# dex:explanation text:"Start the waybill route-list request."
class _StartRoutes(dex.Step[QueryRoutesInput]):
    def get_step_type(self) -> str:
        return "StartSFExpressRoutes"

    def execute(self, context: dex.Context, input: QueryRoutesInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallSFExpress"), input)


# dex:group group-id:sfexpress group-label:"SF Express"
# dex:explanation text:"Persist the waybill route events and complete the Flow."
class _CompleteRoutes(dex.Step[QueryResult[RouteQueryResult]]):
    def get_step_type(self) -> str:
        return "CompleteSFExpressRoutes"

    def execute(self, context: dex.Context, input: QueryResult[RouteQueryResult]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class RoutesFlow(dex.Flow[QueryRoutesInput]):
    """Read one waybill route history through the static SF Express connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("RoutesFlow requires the static SF Express connection")
        self._start = _StartRoutes()
        self._complete = _CompleteRoutes()
        self._call = new_query_routes_step(
            step_type="CallSFExpress",
            step_input_type=QueryRoutesInput,
            annotations={
                "groupId": "sfexpress",
                "groupLabel": "SF Express",
                "explanation": "Read one waybill route industry labels.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable route-history Flow identity."""
        return "SFExpressRoutes"

    def get_steps(self) -> dex.StepList[QueryRoutesInput]:
        """Register the industry labels Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the route-history result."""
        return dex.PersistenceSchema.of(_RESULT)
