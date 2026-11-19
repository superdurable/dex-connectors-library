# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable TextIn getAccountBalance Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.textin import (
    Connection,
    GetAccountBalanceInput,
    GetAccountBalanceOutput,
    new_get_account_balance_step,
)

CONNECTION_NAME = "textin"
_RESULT = dex.Attribute("textin-result", GetAccountBalanceOutput)


def map_to_operation_input(request: GetAccountBalanceInput) -> GetAccountBalanceInput:
    """Forward the provider request."""
    return request


# dex:group group-id:textin group-label:"TextIn"
# dex:explanation text:"Start the TextIn query."
class _StartQuery(dex.Step[GetAccountBalanceInput]):
    def execute(self, context: dex.Context, input: GetAccountBalanceInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:textin group-label:"TextIn"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[GetAccountBalanceOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[GetAccountBalanceOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class GetAccountBalanceFlow(dex.Flow[GetAccountBalanceInput]):
    """Run one TextIn query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("TextIn Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_get_account_balance_step(
            step_type="CallProvider",
            step_input_type=GetAccountBalanceInput,
            annotations={
                "groupId": "textin",
                "groupLabel": "TextIn",
                "explanation": "Run TextIn getAccountBalance.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            fetched=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "GetAccountBalanceTextin"

    def get_steps(self) -> dex.StepList[GetAccountBalanceInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
