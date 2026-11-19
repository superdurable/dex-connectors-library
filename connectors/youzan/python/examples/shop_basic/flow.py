# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read youzan data through the static Dex connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.youzan import Connection, GetShopBasicInput, ShopBasic, new_get_shop_basic_step

CONNECTION_NAME = "youzan"
_OUTCOME = dex.Attribute("youzan-shop-basic-result", ShopBasic)


def map_to_operation_input(input: GetShopBasicInput) -> GetShopBasicInput:
    """Pass the typed request to the provider Query."""
    return input


# dex:group group-id:shop-basic group-label:"ShopBasic"
# dex:explanation text:"Start the youzan Query."
class _StartRequest(dex.Step[GetShopBasicInput]):
    def get_step_type(self) -> str:
        return "StartShopBasic"

    def execute(self, context: dex.Context, input: GetShopBasicInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallShopBasic"), input)


# dex:group group-id:shop-basic group-label:"ShopBasic"
# dex:explanation text:"Persist the provider result and complete the Flow."
class _CompleteResult(dex.Step[QueryResult[ShopBasic]]):
    def get_step_type(self) -> str:
        return "CompleteShopBasic"

    def execute(self, context: dex.Context, input: QueryResult[ShopBasic]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class ShopBasicFlow(dex.Flow[GetShopBasicInput]):
    """Run getShopBasic using the static `youzan` connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("youzan example requires the static connection")
        self._start = _StartRequest()
        self._complete = _CompleteResult()
        self._query = new_get_shop_basic_step(
            step_type="CallShopBasic",
            step_input_type=GetShopBasicInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "youzan",
                "explanation": "Call the youzan getShopBasic Query.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "YouzanShopBasic"

    def get_steps(self) -> dex.StepList[GetShopBasicInput]:
        """Register the start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the typed provider output."""
        return dex.PersistenceSchema.of(_OUTCOME)
