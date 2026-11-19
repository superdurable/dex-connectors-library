# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one TokenFactory response through a durable Dex Query Step."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.tokenfactory import (
    Balance,
    Connection,
    GetBalanceInput,
    new_get_balance_step,
)

CONNECTION_NAME = "tokenfactory"
_RESULT = dex.Attribute("tokenfactory-query-result", Balance)


def map_to_operation_input(input: GetBalanceInput) -> GetBalanceInput:
    """Pass the typed request into the provider Query."""
    return input


# dex:group group-id:query group-label:"Provider query"
# dex:explanation text:"Start the TokenFactory provider Query."
class _StartQuery(dex.Step[GetBalanceInput]):
    def get_step_type(self) -> str:
        return "StartGetBalanceQuery"

    def execute(self, context: dex.Context, input: GetBalanceInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("GetBalanceQuery"), input)


# dex:group group-id:query group-label:"Provider query"
# dex:explanation text:"Persist the validated TokenFactory Query result."
class _CompleteQuery(dex.Step[QueryResult[Balance]]):
    def get_step_type(self) -> str:
        return "CompleteGetBalanceQuery"

    def execute(self, context: dex.Context, input: QueryResult[Balance]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class TokenFactoryGetBalanceFlow(dex.Flow[GetBalanceInput]):
    """Run the TokenFactory Query with the static named connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("TokenFactory Query requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._query = new_get_balance_step(
            step_type="GetBalanceQuery",
            step_input_type=GetBalanceInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "TokenFactory",
                "explanation": "Call TokenFactory GetBalance using the configured credential.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            retrieved=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity for Worker registration."""
        return "TokenFactoryGetBalance"

    def get_steps(self) -> dex.StepList[GetBalanceInput]:
        """Register the start, Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the Query output for later reads."""
        return dex.PersistenceSchema.of(_RESULT)
