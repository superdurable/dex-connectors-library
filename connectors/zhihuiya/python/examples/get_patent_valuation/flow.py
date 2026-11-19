# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Zhihuiya getPatentValuation Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.zhihuiya import (
    Connection,
    GetPatentValuationInput,
    GetPatentValuationOutput,
    new_get_patent_valuation_step,
)

CONNECTION_NAME = "zhihuiya"
_RESULT = dex.Attribute("zhihuiya-result", GetPatentValuationOutput)


def map_to_operation_input(request: GetPatentValuationInput) -> GetPatentValuationInput:
    """Forward the provider request."""
    return request


# dex:group group-id:zhihuiya group-label:"Zhihuiya"
# dex:explanation text:"Start the Zhihuiya query."
class _StartQuery(dex.Step[GetPatentValuationInput]):
    def execute(self, context: dex.Context, input: GetPatentValuationInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:zhihuiya group-label:"Zhihuiya"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[GetPatentValuationOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[GetPatentValuationOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class GetPatentValuationFlow(dex.Flow[GetPatentValuationInput]):
    """Run one Zhihuiya query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Zhihuiya Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_get_patent_valuation_step(
            step_type="CallProvider",
            step_input_type=GetPatentValuationInput,
            annotations={
                "groupId": "zhihuiya",
                "groupLabel": "Zhihuiya",
                "explanation": "Run Zhihuiya getPatentValuation.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "GetPatentValuationZhihuiya"

    def get_steps(self) -> dex.StepList[GetPatentValuationInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
