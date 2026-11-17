# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read Wuliang AI CNY balance through a statically named Dex connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.dieyuyun import (
    Connection,
    GetAccountBalanceInput,
    GetAccountBalanceOutput,
    new_get_account_balance_step,
)

CONNECTION_NAME = "dieyuyun"
_OUTCOME = dex.Attribute("dieyuyun-account-balance-outcome", GetAccountBalanceOutput)


def map_to_operation_input(input: GetAccountBalanceInput) -> GetAccountBalanceInput:
    """Pass the empty request to the provider Query."""
    return input


# dex:group group-id:account-balance group-label:"Account balance"
# dex:explanation text:"Start the Wuliang AI account balance lookup."
class _StartRequest(dex.Step[GetAccountBalanceInput]):
    def get_step_type(self) -> str:
        return "StartGetAccountBalance"

    def execute(self, context: dex.Context, input: GetAccountBalanceInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallGetAccountBalance"), input)


# dex:group group-id:account-balance group-label:"Account balance"
# dex:explanation text:"Persist the exact CNY balance and complete the Flow."
class _CompleteResult(dex.Step[QueryResult[GetAccountBalanceOutput]]):
    def get_step_type(self) -> str:
        return "CompleteGetAccountBalance"

    def execute(self, context: dex.Context, input: QueryResult[GetAccountBalanceOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class AccountBalanceFlow(dex.Flow[GetAccountBalanceInput]):
    """Read available CNY yuan from the `dieyuyun` connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("account balance requires the static dieyuyun connection")
        self._start = _StartRequest()
        self._complete = _CompleteResult()
        self._read = new_get_account_balance_step(
            step_type="CallGetAccountBalance",
            step_input_type=GetAccountBalanceInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "Dieyuyun",
                "explanation": "Call Dieyuyun's getAccountBalance operation.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            fetched=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable type shown in Dex Web."""
        return "DieyuyunAccountBalance"

    def get_steps(self) -> dex.StepList[GetAccountBalanceInput]:
        """Register the start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._read, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the exact CNY balance result."""
        return dex.PersistenceSchema.of(_OUTCOME)
