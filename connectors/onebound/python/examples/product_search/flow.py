# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Onebound product-search Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.onebound import (
    Connection,
    ProductSearchPage,
    SearchProductsInput,
    new_search_products_step,
)

CONNECTION_NAME = "onebound"
_RESULT = dex.Attribute("onebound-product-search", ProductSearchPage)


def map_to_operation_input(request: SearchProductsInput) -> SearchProductsInput:
    """Forward the product page request to Onebound."""
    return request


# dex:group group-id:onebound group-label:"Onebound"
# dex:explanation text:"Start the Onebound product-search request."
class _StartProfile(dex.Step[SearchProductsInput]):
    def get_step_type(self) -> str:
        return "StartOneboundProductSearchPage"

    def execute(self, context: dex.Context, input: SearchProductsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallOnebound"), input)


# dex:group group-id:onebound group-label:"Onebound"
# dex:explanation text:"Persist product page and complete the Flow."
class _CompleteProfile(dex.Step[QueryResult[ProductSearchPage]]):
    def get_step_type(self) -> str:
        return "CompleteOneboundProductSearchPage"

    def execute(self, context: dex.Context, input: QueryResult[ProductSearchPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ProductSearchPageFlow(dex.Flow[SearchProductsInput]):
    """Read one product page through the static Onebound connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("ProductSearchPageFlow requires the static Onebound connection")
        self._start = _StartProfile()
        self._complete = _CompleteProfile()
        self._call = new_search_products_step(
            step_type="CallOnebound",
            step_input_type=SearchProductsInput,
            annotations={
                "groupId": "onebound",
                "groupLabel": "Onebound",
                "explanation": "Search public 1688 listings.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable product-search Flow identity."""
        return "OneboundProductSearchPage"

    def get_steps(self) -> dex.StepList[SearchProductsInput]:
        """Register the product search Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the product-search result."""
        return dex.PersistenceSchema.of(_RESULT)
