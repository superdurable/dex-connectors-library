# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Meituan Delivery order-status Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.meituan import (
    Connection,
    OrderStatusResult,
    QueryOrderStatusInput,
    new_query_order_status_step,
)

CONNECTION_NAME = "meituan"
_RESULT = dex.Attribute("meituan-order-status", OrderStatusResult)


def map_to_operation_input(request: QueryOrderStatusInput) -> QueryOrderStatusInput:
    """Forward the delivery and Meituan order IDs."""
    return request


# dex:group group-id:meituan group-label:"Meituan Delivery"
# dex:explanation text:"Start the Meituan Delivery order-status request."
class _StartOrderStatus(dex.Step[QueryOrderStatusInput]):
    def get_step_type(self) -> str:
        return "StartMeituanOrderStatus"

    def execute(self, context: dex.Context, input: QueryOrderStatusInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallMeituan"), input)


# dex:group group-id:meituan group-label:"Meituan Delivery"
# dex:explanation text:"Persist current order status and complete the Flow."
class _CompleteOrderStatus(dex.Step[QueryResult[OrderStatusResult]]):
    def get_step_type(self) -> str:
        return "CompleteMeituanOrderStatus"

    def execute(self, context: dex.Context, input: QueryResult[OrderStatusResult]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class OrderStatusFlow(dex.Flow[QueryOrderStatusInput]):
    """Read one delivery order through the static Meituan connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("OrderStatusFlow requires the static Meituan Delivery connection")
        self._start = _StartOrderStatus()
        self._complete = _CompleteOrderStatus()
        self._call = new_query_order_status_step(
            step_type="CallMeituan",
            step_input_type=QueryOrderStatusInput,
            annotations={
                "groupId": "meituan",
                "groupLabel": "Meituan Delivery",
                "explanation": "Read one delivery order status.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable order-status Flow identity."""
        return "MeituanOrderStatus"

    def get_steps(self) -> dex.StepList[QueryOrderStatusInput]:
        """Register the order status Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the order-status result."""
        return dex.PersistenceSchema.of(_RESULT)
