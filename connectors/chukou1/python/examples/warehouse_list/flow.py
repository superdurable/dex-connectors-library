# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read Chukou1 overseas warehouses through a statically named Dex connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.chukou1 import (
    Connection,
    ListWarehousesInput,
    WarehousePage,
    new_list_warehouses_step,
)

CONNECTION_NAME = "chukou1"
_OUTCOME = dex.Attribute("chukou1-warehouse-list-outcome", WarehousePage)


def map_to_operation_input(input: ListWarehousesInput) -> ListWarehousesInput:
    """Pass the empty request to the provider Query."""
    return input


# dex:group group-id:warehouse-list group-label:"Warehouse list"
# dex:explanation text:"Start the Chukou1 warehouse lookup."
class _StartRequest(dex.Step[ListWarehousesInput]):
    def get_step_type(self) -> str:
        return "StartListWarehouses"

    def execute(self, context: dex.Context, input: ListWarehousesInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListWarehouses"), input)


# dex:group group-id:warehouse-list group-label:"Warehouse list"
# dex:explanation text:"Record the returned warehouses and complete the Flow."
class _CompleteResult(dex.Step[QueryResult[WarehousePage]]):
    def get_step_type(self) -> str:
        return "CompleteListWarehouses"

    def execute(self, context: dex.Context, input: QueryResult[WarehousePage]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class WarehouseListFlow(dex.Flow[ListWarehousesInput]):
    """Read one page of overseas warehouses from the `chukou1` connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("warehouse list requires the static chukou1 connection")
        self._start = _StartRequest()
        self._complete = _CompleteResult()
        self._read = new_list_warehouses_step(
            step_type="CallListWarehouses",
            step_input_type=ListWarehousesInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "Chukou1",
                "explanation": "Call Chukou1's listWarehouses operation.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable type shown in Dex Web."""
        return "Chukou1WarehouseList"

    def get_steps(self) -> dex.StepList[ListWarehousesInput]:
        """Register the start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._read, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the latest one-page result for reads."""
        return dex.PersistenceSchema.of(_OUTCOME)
