# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read Shumei provider data in a durable Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.shumei import Balance, Connection, GetBalanceInput, new_get_balance_step

CONNECTION_NAME = "shumei"
_RESULT = dex.Attribute("shumei-query-result", Balance)


def map_to_operation_input(input: GetBalanceInput) -> GetBalanceInput:
    """Pass the application Query input to the provider."""
    return input


# dex:group group-id:shumei group-label:"Shumei"
# dex:explanation text:"Start the Shumei Query."
class _StartQuery(dex.Step[GetBalanceInput]):
    def get_step_type(self) -> str:
        return "StartShumeiQuery"

    def execute(self, context: dex.Context, input: GetBalanceInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallShumei"), input)


# dex:group group-id:shumei group-label:"Shumei"
# dex:explanation text:"Persist the Shumei Query result and complete the Flow."
class _CompleteQuery(dex.Step[QueryResult[Balance]]):
    def get_step_type(self) -> str:
        return "CompleteShumeiQuery"

    def execute(self, context: dex.Context, input: QueryResult[Balance]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ProviderQueryFlow(dex.Flow[GetBalanceInput]):
    """Read the configured Shumei account through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("ProviderQueryFlow requires the static Shumei connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_get_balance_step(
            step_type="CallShumei",
            step_input_type=GetBalanceInput,
            annotations={
                "groupId": "shumei",
                "groupLabel": "Shumei",
                "explanation": "Read Shumei account data.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            retrieved=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Shumei Query Flow identity."""
        return "ShumeiProviderQuery"

    def get_steps(self) -> dex.StepList[GetBalanceInput]:
        """Register start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider Query output."""
        return dex.PersistenceSchema.of(_RESULT)
