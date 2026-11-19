# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one Minimax response through a durable Dex Query Step."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.minimax import (
    Connection,
    GetTokenPlanRemainsInput,
    GetTokenPlanRemainsOutput,
    new_get_token_plan_remains_step,
)

CONNECTION_NAME = "minimax"
_RESULT = dex.Attribute("minimax-query-result", GetTokenPlanRemainsOutput)


def map_to_operation_input(input: GetTokenPlanRemainsInput) -> GetTokenPlanRemainsInput:
    """Pass the typed request into the provider Query."""
    return input


# dex:group group-id:query group-label:"Provider query"
# dex:explanation text:"Start the Minimax provider Query."
class _StartQuery(dex.Step[GetTokenPlanRemainsInput]):
    def get_step_type(self) -> str:
        return "StartGetTokenPlanRemainsQuery"

    def execute(self, context: dex.Context, input: GetTokenPlanRemainsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("GetTokenPlanRemainsQuery"), input)


# dex:group group-id:query group-label:"Provider query"
# dex:explanation text:"Persist the validated Minimax Query result."
class _CompleteQuery(dex.Step[QueryResult[GetTokenPlanRemainsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteGetTokenPlanRemainsQuery"

    def execute(
        self, context: dex.Context, input: QueryResult[GetTokenPlanRemainsOutput]
    ) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class MinimaxGetTokenPlanRemainsFlow(dex.Flow[GetTokenPlanRemainsInput]):
    """Run the Minimax Query with the static named connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Minimax Query requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._query = new_get_token_plan_remains_step(
            step_type="GetTokenPlanRemainsQuery",
            step_input_type=GetTokenPlanRemainsInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "Minimax",
                "explanation": "Call Minimax GetTokenPlanRemains using the configured credential.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            fetched=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity for Worker registration."""
        return "MinimaxGetTokenPlanRemains"

    def get_steps(self) -> dex.StepList[GetTokenPlanRemainsInput]:
        """Register the start, Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the Query output for later reads."""
        return dex.PersistenceSchema.of(_RESULT)
