# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Fanyigou account balance Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.fanyigou import (
    Connection,
    GetAccountBalanceInput,
    GetAccountBalanceOutput,
    new_get_account_balance_step,
)

CONNECTION_NAME = "fanyigou"
_RESULT = dex.Attribute("fanyigou-account-balance", GetAccountBalanceOutput)


def map_to_operation_input(request: GetAccountBalanceInput) -> GetAccountBalanceInput:
    """Forward the account balance request."""
    return request


# dex:group group-id:fanyigou group-label:"Fanyigou"
# dex:explanation text:"Start one Fanyigou account balance query."
class _StartAccountBalance(dex.Step[GetAccountBalanceInput]):
    def execute(self, context: dex.Context, input: GetAccountBalanceInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallFanyigou"), input)


# dex:group group-id:fanyigou group-label:"Fanyigou"
# dex:explanation text:"Persist coarse account balance before completion."
class _CompleteAccountBalance(dex.Step[QueryResult[GetAccountBalanceOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[GetAccountBalanceOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class AccountBalanceFlow(dex.Flow[GetAccountBalanceInput]):
    """Read the document balance through the static Fanyigou connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("AccountBalanceFlow requires the static Fanyigou connection")
        self._start = _StartAccountBalance()
        self._complete = _CompleteAccountBalance()
        self._call = new_get_account_balance_step(
            step_type="CallFanyigou",
            step_input_type=GetAccountBalanceInput,
            annotations={
                "groupId": "fanyigou",
                "groupLabel": "Fanyigou",
                "explanation": "Read the document-translation account balance.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable account balance Flow identity."""
        return "FanyigouAccountBalance"

    def get_steps(self) -> dex.StepList[GetAccountBalanceInput]:
        """Register the account balance Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the returned account balance only."""
        return dex.PersistenceSchema.of(_RESULT)
