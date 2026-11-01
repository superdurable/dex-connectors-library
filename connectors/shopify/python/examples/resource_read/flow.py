# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Shopify resource read Flow using the listPayments Query."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.shopify import Connection, GetOrderInput, Order, new_get_order_step

FLOW_TYPE = "ShopifyResourceRead"
CONNECTION_NAME = "shopify"
_READ_STEP_TYPE = "ReadShopifyResource"


@dataclass(frozen=True, slots=True)
class ResourceReadRequest:
    """Positive Shopify order ID to read."""

    order_id: int = field(default=0, metadata={"dex_json_name": "orderID"})


@dataclass(frozen=True, slots=True)
class ResourceReadOutcome:
    """The order Shopify returned."""

    value: Order = field(default_factory=Order)


_OUTCOME = dex.Attribute("shopify-resource-read-outcome", ResourceReadOutcome)


def map_to_operation_input(request: ResourceReadRequest) -> GetOrderInput:
    """Read the requested order."""
    return GetOrderInput(request.order_id)


# dex:group group-id:resource-read group-label:"Resource read"
# dex:explanation text:"Validate the request before reading the provider."
class _PrepareRequest(dex.Step[ResourceReadRequest]):
    def get_step_type(self) -> str:
        return "PrepareResourceRead"

    def execute(self, context: dex.Context, input: ResourceReadRequest) -> dex.StepDecision:
        if input.order_id <= 0:
            return dex.force_fail("OrderID must be positive")
        return dex.go_to(dex.StepRef(_READ_STEP_TYPE), input)


# dex:group group-id:resource-read group-label:"Resource read"
# dex:explanation text:"Record the order and complete the Flow."
class _CompleteRead(dex.Step[QueryResult[Order]]):
    def get_step_type(self) -> str:
        return "CompleteResourceRead"

    def execute(self, context: dex.Context, input: QueryResult[Order]) -> dex.StepDecision:
        outcome = ResourceReadOutcome(input.value)
        _OUTCOME.set(context, outcome)
        return dex.graceful_complete(outcome)


class ResourceReadFlow(dex.Flow[ResourceReadRequest]):
    """Read one Shopify order through the static shopify connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("resource read requires the static shopify connection")
        self._prepare = _PrepareRequest()
        self._complete = _CompleteRead()
        self._read = new_get_order_step(
            step_type=_READ_STEP_TYPE,
            step_input_type=ResourceReadRequest,
            annotations={
                "groupId": "shopify",
                "groupLabel": "Shopify",
                "explanation": "Read the requested Shopify resource.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[ResourceReadRequest]:
        """Register request preparation, the Shopify read, and completion Steps."""
        return dex.StepList.start_step(self._prepare).other_steps(self._read, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the order outcome."""
        return dex.PersistenceSchema.of(_OUTCOME)
