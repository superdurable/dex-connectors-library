# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read sendcloud data through the static Dex connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.sendcloud import (
    Connection,
    GetAccountBalanceInput,
    GetAccountBalanceOutput,
    new_get_account_balance_step,
)

CONNECTION_NAME = "sendcloud"
_OUTCOME = dex.Attribute("sendcloud-account-balance-result", GetAccountBalanceOutput)


def map_to_operation_input(input: GetAccountBalanceInput) -> GetAccountBalanceInput:
    """Pass the typed request to the provider Query."""
    return input


# dex:group group-id:account-balance group-label:"AccountBalance"
# dex:explanation text:"Start the sendcloud Query."
class _StartRequest(dex.Step[GetAccountBalanceInput]):
    def get_step_type(self) -> str:
        return "StartAccountBalance"

    def execute(self, context: dex.Context, input: GetAccountBalanceInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallAccountBalance"), input)


# dex:group group-id:account-balance group-label:"AccountBalance"
# dex:explanation text:"Persist the provider result and complete the Flow."
class _CompleteResult(dex.Step[QueryResult[GetAccountBalanceOutput]]):
    def get_step_type(self) -> str:
        return "CompleteAccountBalance"

    def execute(self, context: dex.Context, input: QueryResult[GetAccountBalanceOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class AccountBalanceFlow(dex.Flow[GetAccountBalanceInput]):
    """Run getAccountBalance using the static `sendcloud` connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("sendcloud example requires the static connection")
        self._start = _StartRequest()
        self._complete = _CompleteResult()
        self._query = new_get_account_balance_step(
            step_type="CallAccountBalance",
            step_input_type=GetAccountBalanceInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "sendcloud",
                "explanation": "Call the sendcloud getAccountBalance Query.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            retrieved=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "SendcloudAccountBalance"

    def get_steps(self) -> dex.StepList[GetAccountBalanceInput]:
        """Register the start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the typed provider output."""
        return dex.PersistenceSchema.of(_OUTCOME)
