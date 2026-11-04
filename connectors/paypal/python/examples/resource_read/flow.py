# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable PayPal resource read Flow using the getOrder Query."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.paypal import Connection, GetOrderInput, Order, new_get_order_step

FLOW_TYPE = "PayPalResourceRead"
CONNECTION_NAME = "paypal"
_READ_STEP_TYPE = "ReadPayPalResource"


@dataclass(frozen=True, slots=True)
class ResourceReadRequest:
    """The checkout order ID to read."""

    order_id: str = field(default="", metadata={"dex_json_name": "orderID"})


@dataclass(frozen=True, slots=True)
class ResourceReadOutcome:
    """The order PayPal returned."""

    value: Order = field(default_factory=Order)


_OUTCOME = dex.Attribute("paypal-resource-read-outcome", ResourceReadOutcome)


def map_to_operation_input(request: ResourceReadRequest) -> GetOrderInput:
    """Read the requested checkout order."""
    return GetOrderInput(request.order_id)


# dex:group group-id:resource-read group-label:"Resource read"
# dex:explanation text:"Validate the request before reading the provider."
class _PrepareRequest(dex.Step[ResourceReadRequest]):
    def get_step_type(self) -> str:
        return "PrepareResourceRead"

    def execute(self, context: dex.Context, input: ResourceReadRequest) -> dex.StepDecision:
        if not input.order_id:
            return dex.force_fail("OrderID is required")
        return dex.go_to(dex.StepRef(_READ_STEP_TYPE), input)


# dex:group group-id:resource-result group-label:"Resource result"
# dex:explanation text:"Persist and return the provider result as the Flow outcome."
class _CompleteRead(dex.Step[QueryResult[Order]]):
    def get_step_type(self) -> str:
        return "CompleteResourceRead"

    def execute(self, context: dex.Context, input: QueryResult[Order]) -> dex.StepDecision:
        outcome = ResourceReadOutcome(input.value)
        _OUTCOME.set(context, outcome)
        return dex.graceful_complete(outcome)


class ResourceReadFlow(dex.Flow[ResourceReadRequest]):
    """Read one PayPal checkout order through the static paypal connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("resource read requires the static paypal connection")
        self._prepare = _PrepareRequest()
        self._complete = _CompleteRead()
        self._read = new_get_order_step(
            step_type=_READ_STEP_TYPE,
            step_input_type=ResourceReadRequest,
            annotations={
                "groupId": "paypal",
                "groupLabel": "PayPal",
                "explanation": "Read the requested PayPal resource.",
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
        """Register request preparation, the PayPal read, and completion Steps."""
        return dex.StepList.start_step(self._prepare).other_steps(self._read, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the order outcome."""
        return dex.PersistenceSchema.of(_OUTCOME)
