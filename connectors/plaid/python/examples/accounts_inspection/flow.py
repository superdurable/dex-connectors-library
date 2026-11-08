# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Plaid accounts inspection Flow using getAccounts."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.plaid import (
    AccountsSnapshot,
    Connection,
    GetAccountsInput,
    new_get_accounts_step,
)

FLOW_TYPE = "PlaidAccountsInspectionPython"
CONNECTION_NAME = "plaid"
_CALL_STEP_TYPE = "CallPlaid"
_RESULT = dex.Attribute("plaid-accounts", AccountsSnapshot)


def map_to_operation_input(request: GetAccountsInput) -> GetAccountsInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:plaid group-label:"Plaid"
# dex:explanation text:"Start the Plaid request."
class _StartRequest(dex.Step[GetAccountsInput]):
    def get_step_type(self) -> str:
        return "StartPlaidRequest"

    def execute(self, context: dex.Context, input: GetAccountsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallPlaid"), input)


# dex:group group-id:plaid group-label:"Plaid"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[AccountsSnapshot]]):
    def get_step_type(self) -> str:
        return "CompletePlaidRequest"

    def execute(self, context: dex.Context, input: QueryResult[AccountsSnapshot]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class AccountsInspectionFlow(dex.Flow[GetAccountsInput]):
    """Read the linked accounts through the static plaid connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("accounts inspection requires the static plaid connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_accounts_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetAccountsInput,
            annotations={
                "groupId": "plaid",
                "groupLabel": "Plaid",
                "explanation": "Read linked accounts from the connected Plaid Item.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "PlaidAccountsInspectionPython"

    def get_steps(self) -> dex.StepList[GetAccountsInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
