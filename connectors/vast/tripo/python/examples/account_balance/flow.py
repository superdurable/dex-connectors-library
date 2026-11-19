# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Tripo get account balance Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.vast_tripo import (
    Connection,
    GetAccountBalanceInput,
    GetAccountBalanceOutput,
    new_get_account_balance_step,
)

CONNECTION_NAME = "tripo"
_RESULT = dex.Attribute("tripo-get-account-balance-result", GetAccountBalanceOutput)


def map_to_operation_input(request: GetAccountBalanceInput) -> GetAccountBalanceInput:
    """Forward the typed request to the provider Query."""
    return request


# dex:group group-id:tripo group-label:"Tripo"
# dex:explanation text:"Start the Tripo get account balance request."
class _StartQuery(dex.Step[GetAccountBalanceInput]):
    def get_step_type(self) -> str:
        return "StartTripoAccountBalance"

    def execute(self, context: dex.Context, input: GetAccountBalanceInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallTripoAccountBalance"), input)


# dex:group group-id:tripo group-label:"Tripo"
# dex:explanation text:"Persist the provider result and complete the Flow."
class _CompleteQuery(dex.Step[QueryResult[GetAccountBalanceOutput]]):
    def get_step_type(self) -> str:
        return "CompleteTripoAccountBalance"

    def execute(self, context: dex.Context, input: QueryResult[GetAccountBalanceOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class TripoAccountBalanceFlow(dex.Flow[GetAccountBalanceInput]):
    """Run the get account balance Query with a static named connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("TripoAccountBalanceFlow requires the static Tripo connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_get_account_balance_step(
            step_type="CallTripoAccountBalance",
            step_input_type=GetAccountBalanceInput,
            annotations={
                "groupId": "tripo",
                "groupLabel": "Tripo",
                "explanation": "Get account balance with the configured Tripo connection.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            fetched=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable get account balance Flow identity."""
        return "TripoAccountBalance"

    def get_steps(self) -> dex.StepList[GetAccountBalanceInput]:
        """Register the application and connector Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the bounded get account balance result."""
        return dex.PersistenceSchema.of(_RESULT)
