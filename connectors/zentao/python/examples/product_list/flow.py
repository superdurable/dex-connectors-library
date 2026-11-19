# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read zentao through one durable provider Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.zentao import Connection, ListProductsInput, ProductList, new_list_products_step

CONNECTION_NAME = "zentao"
_OUTCOME = dex.Attribute("zentao-list_products-outcome", ProductList)


def map_to_operation_input(input: ListProductsInput) -> ListProductsInput:
    """Forward the typed Flow input to the provider Query."""
    return input


# dex:group group-id:read group-label:"Provider read"
# dex:explanation text:"Begin the zentao read with the configured connection."
class _Start(dex.Step[ListProductsInput]):
    def get_step_type(self) -> str:
        return "StartListProducts"

    def execute(self, context: dex.Context, input: ListProductsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("ListProductsQuery"), input)


# dex:group group-id:read group-label:"Provider read"
# dex:explanation text:"Persist the provider result and complete the Flow."
class _Complete(dex.Step[QueryResult[ProductList]]):
    def get_step_type(self) -> str:
        return "CompleteListProducts"

    def execute(self, context: dex.Context, input: QueryResult[ProductList]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListProductsFlow(dex.Flow[ListProductsInput]):
    """Durably read zentao with its static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("zentao Flow requires its static connection")
        self._start = _Start()
        self._complete = _Complete()
        self._query = new_list_products_step(
            step_type="ListProductsQuery",
            step_input_type=ListProductsInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "zentao",
                "explanation": "Call zentao listProducts.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity shown in Dex Web."""
        return "ZentaoListProducts"

    def get_steps(self) -> dex.StepList[ListProductsInput]:
        """Register the start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable provider result Attribute."""
        return dex.PersistenceSchema.of(_OUTCOME)
