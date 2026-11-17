# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one authorized EasyAPI invoicing company's service status."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.easyapi import Connection, QueryShopInput, ShopResult, new_query_shop_step

CONNECTION_NAME = "easyapi"
shop_result_attribute = dex.Attribute("easyapi-shop-result-python", ShopResult)


# dex:group group-id:shop group-label:"Company service"
# dex:explanation text:"Record the non-private service status and complete the Flow."
class RecordShopResult(dex.Step[QueryResult[ShopResult]]):
    """Persist company ID, name, API state, and expiry only."""

    def get_step_type(self) -> str:
        """Return the stable recording Step identity."""
        return "RecordEasyAPIShopResult"

    def execute(self, context: dex.Context, input: QueryResult[ShopResult]) -> dex.StepDecision:
        """Record the restricted service status output."""
        shop_result_attribute.set(context, input.value)
        return dex.graceful_complete(input.value)


class ShopFlow(dex.Flow[QueryShopInput]):
    """Read an authorized invoicing company's service status."""

    def __init__(self, connection: Connection) -> None:
        """Bind the provider Query and completion Step."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("shop Flow requires the static EasyAPI connection")
        self.record = RecordShopResult()
        self.read = new_query_shop_step(
            step_type="QueryEasyAPIShop",
            step_input_type=QueryShopInput,
            annotations={
                "groupId": "shop",
                "groupLabel": "Company service",
                "explanation": "GET the authorized company's service configuration.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            found=self.record,
        )

    def get_flow_type(self) -> str:
        """Return the durable Flow identity."""
        return "EasyAPIShopPython"

    def get_steps(self) -> dex.StepList[QueryShopInput]:
        """Register both provider and recording Steps."""
        return dex.StepList.start_step(self.read).other_steps(self.record)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the restricted company result Attribute."""
        return dex.PersistenceSchema.of(shop_result_attribute)
