# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read yunpian through one durable provider Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.yunpian import (
    AccountBalance,
    Connection,
    GetAccountBalanceInput,
    new_get_account_balance_step,
)

CONNECTION_NAME = "yunpian"
_OUTCOME = dex.Attribute("yunpian-get_account_balance-outcome", AccountBalance)


def map_to_operation_input(input: GetAccountBalanceInput) -> GetAccountBalanceInput:
    """Forward the typed Flow input to the provider Query."""
    return input


# dex:group group-id:read group-label:"Provider read"
# dex:explanation text:"Begin the yunpian read with the configured connection."
class _Start(dex.Step[GetAccountBalanceInput]):
    def get_step_type(self) -> str:
        return "StartGetAccountBalance"

    def execute(self, context: dex.Context, input: GetAccountBalanceInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("GetAccountBalanceQuery"), input)


# dex:group group-id:read group-label:"Provider read"
# dex:explanation text:"Persist the provider result and complete the Flow."
class _Complete(dex.Step[QueryResult[AccountBalance]]):
    def get_step_type(self) -> str:
        return "CompleteGetAccountBalance"

    def execute(self, context: dex.Context, input: QueryResult[AccountBalance]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class GetAccountBalanceFlow(dex.Flow[GetAccountBalanceInput]):
    """Durably read yunpian with its static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("yunpian Flow requires its static connection")
        self._start = _Start()
        self._complete = _Complete()
        self._query = new_get_account_balance_step(
            step_type="GetAccountBalanceQuery",
            step_input_type=GetAccountBalanceInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "yunpian",
                "explanation": "Call yunpian getAccountBalance.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            read=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity shown in Dex Web."""
        return "YunpianGetAccountBalance"

    def get_steps(self) -> dex.StepList[GetAccountBalanceInput]:
        """Register the start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable provider result Attribute."""
        return dex.PersistenceSchema.of(_OUTCOME)
