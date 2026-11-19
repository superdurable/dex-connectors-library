# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable JisuAPI listProvinces Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.jisuapi import (
    Connection,
    ListProvincesInput,
    ListProvincesOutput,
    new_list_provinces_step,
)

CONNECTION_NAME = "jisuapi"
_RESULT = dex.Attribute("jisuapi-result", ListProvincesOutput)


def map_to_operation_input(request: ListProvincesInput) -> ListProvincesInput:
    """Forward the provider request."""
    return request


# dex:group group-id:jisuapi group-label:"JisuAPI"
# dex:explanation text:"Start the JisuAPI query."
class _StartQuery(dex.Step[ListProvincesInput]):
    def execute(self, context: dex.Context, input: ListProvincesInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:jisuapi group-label:"JisuAPI"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[ListProvincesOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[ListProvincesOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListProvincesFlow(dex.Flow[ListProvincesInput]):
    """Run one JisuAPI query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("JisuAPI Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_list_provinces_step(
            step_type="CallProvider",
            step_input_type=ListProvincesInput,
            annotations={
                "groupId": "jisuapi",
                "groupLabel": "JisuAPI",
                "explanation": "Run JisuAPI listProvinces.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "ListProvincesJisuAPI"

    def get_steps(self) -> dex.StepList[ListProvincesInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
