# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable XiaoeTech list_root_goods_categories Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.xiaoetech import (
    Connection,
    ListRootGoodsCategoriesInput,
    ListRootGoodsCategoriesOutput,
    new_list_root_goods_categories_step,
)

CONNECTION_NAME = "xiaoetech"
_RESULT = dex.Attribute("xiaoetech-result", ListRootGoodsCategoriesOutput)


def map_to_operation_input(request: ListRootGoodsCategoriesInput) -> ListRootGoodsCategoriesInput:
    """Forward the provider request."""
    return request


# dex:group group-id:xiaoetech group-label:"XiaoeTech"
# dex:explanation text:"Start the XiaoeTech query."
class _StartQuery(dex.Step[ListRootGoodsCategoriesInput]):
    def execute(self, context: dex.Context, input: ListRootGoodsCategoriesInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:xiaoetech group-label:"XiaoeTech"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[ListRootGoodsCategoriesOutput]]):
    def execute(
        self, context: dex.Context, input: QueryResult[ListRootGoodsCategoriesOutput]
    ) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListRootGoodsCategoriesFlow(dex.Flow[ListRootGoodsCategoriesInput]):
    """Run one XiaoeTech query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("XiaoeTech Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_list_root_goods_categories_step(
            step_type="CallProvider",
            step_input_type=ListRootGoodsCategoriesInput,
            annotations={
                "groupId": "xiaoetech",
                "groupLabel": "XiaoeTech",
                "explanation": "Run XiaoeTech list_root_goods_categories.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "ListRootGoodsCategoriesXiaoeTech"

    def get_steps(self) -> dex.StepList[ListRootGoodsCategoriesInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
