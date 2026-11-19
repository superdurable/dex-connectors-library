# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable SMZDM product-search Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.smzdm import (
    Connection,
    ProductPage,
    SearchProductsInput,
    new_search_products_step,
)

CONNECTION_NAME = "smzdm"
_RESULT = dex.Attribute("smzdm-product-search", ProductPage)


def map_to_operation_input(request: SearchProductsInput) -> SearchProductsInput:
    """Forward the product search ID to SMZDM."""
    return request


# dex:group group-id:smzdm group-label:"SMZDM"
# dex:explanation text:"Start the product search-list request."
class _StartProducts(dex.Step[SearchProductsInput]):
    def get_step_type(self) -> str:
        return "StartSMZDMProducts"

    def execute(self, context: dex.Context, input: SearchProductsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallSMZDM"), input)


# dex:group group-id:smzdm group-label:"SMZDM"
# dex:explanation text:"Persist the product search events and complete the Flow."
class _CompleteProducts(dex.Step[QueryResult[ProductPage]]):
    def get_step_type(self) -> str:
        return "CompleteSMZDMProducts"

    def execute(self, context: dex.Context, input: QueryResult[ProductPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ProductsFlow(dex.Flow[SearchProductsInput]):
    """Read one product search history through the static SMZDM connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("ProductsFlow requires the static SMZDM connection")
        self._start = _StartProducts()
        self._complete = _CompleteProducts()
        self._call = new_search_products_step(
            step_type="CallSMZDM",
            step_input_type=SearchProductsInput,
            annotations={
                "groupId": "smzdm",
                "groupLabel": "SMZDM",
                "explanation": "Read one product search page.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable product-search Flow identity."""
        return "SMZDMProducts"

    def get_steps(self) -> dex.StepList[SearchProductsInput]:
        """Register the product search Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the product-search result."""
        return dex.PersistenceSchema.of(_RESULT)
