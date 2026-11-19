# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read Shoplazza provider data in a durable Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.shoplazza import Connection, GetShopInput, GetShopOutput, new_get_shop_step

CONNECTION_NAME = "shoplazza"
_RESULT = dex.Attribute("shoplazza-query-result", GetShopOutput)


def map_to_operation_input(input: GetShopInput) -> GetShopInput:
    """Pass the application Query input to the provider."""
    return input


# dex:group group-id:shoplazza group-label:"Shoplazza"
# dex:explanation text:"Start the Shoplazza Query."
class _StartQuery(dex.Step[GetShopInput]):
    def get_step_type(self) -> str:
        return "StartShoplazzaQuery"

    def execute(self, context: dex.Context, input: GetShopInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallShoplazza"), input)


# dex:group group-id:shoplazza group-label:"Shoplazza"
# dex:explanation text:"Persist the Shoplazza Query result and complete the Flow."
class _CompleteQuery(dex.Step[QueryResult[GetShopOutput]]):
    def get_step_type(self) -> str:
        return "CompleteShoplazzaQuery"

    def execute(self, context: dex.Context, input: QueryResult[GetShopOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ProviderQueryFlow(dex.Flow[GetShopInput]):
    """Read the configured Shoplazza account through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("ProviderQueryFlow requires the static Shoplazza connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_get_shop_step(
            step_type="CallShoplazza",
            step_input_type=GetShopInput,
            annotations={
                "groupId": "shoplazza",
                "groupLabel": "Shoplazza",
                "explanation": "Read Shoplazza account data.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Shoplazza Query Flow identity."""
        return "ShoplazzaProviderQuery"

    def get_steps(self) -> dex.StepList[GetShopInput]:
        """Register start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider Query output."""
        return dex.PersistenceSchema.of(_RESULT)
