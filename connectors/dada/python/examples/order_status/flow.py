# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one Dada delivery order's status through a Dex Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.dada import (
    Connection,
    OrderStatus,
    QueryOrderStatusInput,
    new_query_order_status_step,
)

CONNECTION_NAME = "dada"
order_status_attribute = dex.Attribute("dada-order-status-python", OrderStatus)


# dex:group group-id:status group-label:"Order status"
# dex:explanation text:"Record the returned delivery state and complete the Flow."
class RecordOrderStatus(dex.Step[QueryResult[OrderStatus]]):
    """Persist only order ID, status code, and status label."""

    def get_step_type(self) -> str:
        """Return the stable recording Step identity."""
        return "RecordDadaOrderStatus"

    def execute(self, context: dex.Context, input: QueryResult[OrderStatus]) -> dex.StepDecision:
        """Record the non-private delivery status."""
        order_status_attribute.set(context, input.value)
        return dex.graceful_complete(input.value)


class OrderStatusFlow(dex.Flow[QueryOrderStatusInput]):
    """Read the connected Dada merchant's current order status."""

    def __init__(self, connection: Connection) -> None:
        """Bind the provider Query and completion Step."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("order status Flow requires the static Dada connection")
        self.record = RecordOrderStatus()
        self.read = new_query_order_status_step(
            step_type="QueryDadaOrderStatus",
            step_input_type=QueryOrderStatusInput,
            annotations={
                "groupId": "status",
                "groupLabel": "Order status",
                "explanation": "Sign and POST the read-only order status query.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            found=self.record,
        )

    def get_flow_type(self) -> str:
        """Return the durable Flow identity."""
        return "DadaOrderStatusPython"

    def get_steps(self) -> dex.StepList[QueryOrderStatusInput]:
        """Register both provider and recording Steps."""
        return dex.StepList.start_step(self.read).other_steps(self.record)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the non-private order status Attribute."""
        return dex.PersistenceSchema.of(order_status_attribute)
