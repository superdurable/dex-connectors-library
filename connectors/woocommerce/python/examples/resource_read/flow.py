# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable WooCommerce resource read Flow using the listPayments Query."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.woocommerce import Connection, ListOrdersInput, OrderPage, new_list_orders_step

FLOW_TYPE = "WooCommerceResourceRead"
CONNECTION_NAME = "woocommerce"
_READ_STEP_TYPE = "ReadWooCommerceResource"


@dataclass(frozen=True, slots=True)
class ResourceReadRequest:
    """Order page to read; zero means WooCommerce's first page."""

    page: int = 0


@dataclass(frozen=True, slots=True)
class ResourceReadOutcome:
    """The order page WooCommerce returned."""

    value: OrderPage = field(default_factory=OrderPage)


_OUTCOME = dex.Attribute("woocommerce-resource-read-outcome", ResourceReadOutcome)


def map_to_operation_input(request: ResourceReadRequest) -> ListOrdersInput:
    """Read the requested order page."""
    return ListOrdersInput(page=request.page)


# dex:group group-id:resource-read group-label:"Resource read"
# dex:explanation text:"Validate the request before reading the provider."
class _PrepareRequest(dex.Step[ResourceReadRequest]):
    def get_step_type(self) -> str:
        return "PrepareResourceRead"

    def execute(self, context: dex.Context, input: ResourceReadRequest) -> dex.StepDecision:
        if input.page < 0:
            return dex.force_fail("page must not be negative")
        return dex.go_to(dex.StepRef(_READ_STEP_TYPE), input)


# dex:group group-id:resource-read group-label:"Resource read"
# dex:explanation text:"Record the order page and complete the Flow."
class _CompleteRead(dex.Step[QueryResult[OrderPage]]):
    def get_step_type(self) -> str:
        return "CompleteResourceRead"

    def execute(self, context: dex.Context, input: QueryResult[OrderPage]) -> dex.StepDecision:
        outcome = ResourceReadOutcome(input.value)
        _OUTCOME.set(context, outcome)
        return dex.graceful_complete(outcome)


class ResourceReadFlow(dex.Flow[ResourceReadRequest]):
    """Read one page of WooCommerce orders through the static woocommerce connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("resource read requires the static woocommerce connection")
        self._prepare = _PrepareRequest()
        self._complete = _CompleteRead()
        self._read = new_list_orders_step(
            step_type=_READ_STEP_TYPE,
            step_input_type=ResourceReadRequest,
            annotations={
                "groupId": "woocommerce",
                "groupLabel": "WooCommerce",
                "explanation": "Read the requested WooCommerce resource.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[ResourceReadRequest]:
        """Register request preparation, the WooCommerce read, and completion Steps."""
        return dex.StepList.start_step(self._prepare).other_steps(self._read, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the order page outcome."""
        return dex.PersistenceSchema.of(_OUTCOME)
