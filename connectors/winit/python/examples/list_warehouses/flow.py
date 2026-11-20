# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Winit list_warehouses Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.winit import (
    Connection,
    ListWarehousesInput,
    WarehousePage,
    new_list_warehouses_step,
)

CONNECTION_NAME = "winit"
_RESULT = dex.Attribute("winit-result", WarehousePage)


def map_to_operation_input(request: ListWarehousesInput) -> ListWarehousesInput:
    """Forward the provider request."""
    return request


# dex:group group-id:winit group-label:"Winit"
# dex:explanation text:"Start the Winit query."
class _StartQuery(dex.Step[ListWarehousesInput]):
    def execute(self, context: dex.Context, input: ListWarehousesInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:winit group-label:"Winit"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[WarehousePage]]):
    def execute(self, context: dex.Context, input: QueryResult[WarehousePage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListWarehousesFlow(dex.Flow[ListWarehousesInput]):
    """Run one Winit query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Winit Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_list_warehouses_step(
            step_type="CallProvider",
            step_input_type=ListWarehousesInput,
            annotations={
                "groupId": "winit",
                "groupLabel": "Winit",
                "explanation": "Run Winit list_warehouses.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "ListWarehousesWinit"

    def get_steps(self) -> dex.StepList[ListWarehousesInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
