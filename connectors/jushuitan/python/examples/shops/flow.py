# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Jushuitan shops Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.jushuitan import (
    Connection,
    ListShopsInput,
    ListShopsOutput,
    new_list_shops_step,
)

CONNECTION_NAME = "jushuitan"
_RESULT = dex.Attribute("jushuitan-shops", ListShopsOutput)


def map_to_operation_input(request: ListShopsInput) -> ListShopsInput:
    """Forward the requested one-based shop page."""
    return request


# dex:group group-id:jushuitan group-label:"Jushuitan"
# dex:explanation text:"Start the Jushuitan shops request."
class _StartShops(dex.Step[ListShopsInput]):
    def get_step_type(self) -> str:
        return "StartJushuitanShops"

    def execute(self, context: dex.Context, input: ListShopsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallJushuitan"), input)


# dex:group group-id:jushuitan group-label:"Jushuitan"
# dex:explanation text:"Persist the shop page and complete the Flow."
class _CompleteShops(dex.Step[QueryResult[ListShopsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteJushuitanShops"

    def execute(self, context: dex.Context, input: QueryResult[ListShopsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ShopsFlow(dex.Flow[ListShopsInput]):
    """List one page of shops through the static Jushuitan connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("ShopsFlow requires the static Jushuitan connection")
        self._start = _StartShops()
        self._complete = _CompleteShops()
        self._call = new_list_shops_step(
            step_type="CallJushuitan",
            step_input_type=ListShopsInput,
            annotations={
                "groupId": "jushuitan",
                "groupLabel": "Jushuitan",
                "explanation": "List enabled shops for one page.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable shops Flow identity."""
        return "JushuitanShops"

    def get_steps(self) -> dex.StepList[ListShopsInput]:
        """Register the shop-list Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the shops result."""
        return dex.PersistenceSchema.of(_RESULT)
