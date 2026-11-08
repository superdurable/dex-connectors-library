# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable AWeber page Flow using the listAccounts Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.aweber import (
    AccountsPage,
    Connection,
    ListAccountsInput,
    new_list_accounts_step,
)

FLOW_TYPE = "AWeberAccountsListPython"
CONNECTION_NAME = "aweber"
_CALL_STEP_TYPE = "CallListAccounts"
_RESULT = dex.Attribute("aweber-python-page", AccountsPage)


def map_to_operation_input(request: ListAccountsInput) -> ListAccountsInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:aweber group-label:"AWeber"
# dex:explanation text:"Start the AWeber request."
class _StartRequest(dex.Step[ListAccountsInput]):
    def get_step_type(self) -> str:
        return "StartListAccounts"

    def execute(self, context: dex.Context, input: ListAccountsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListAccounts"), input)


# dex:group group-id:aweber group-label:"AWeber"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[AccountsPage]]):
    def get_step_type(self) -> str:
        return "CompleteListAccounts"

    def execute(self, context: dex.Context, input: QueryResult[AccountsPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class AccountsFlow(dex.Flow[ListAccountsInput]):
    """Run one listAccounts read through the static aweber connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the accounts lookup Flow requires the static aweber connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_accounts_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListAccountsInput,
            annotations={
                "groupId": "aweber",
                "groupLabel": "AWeber",
                "explanation": "Read one AWeber page.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "AWeberAccountsListPython"

    def get_steps(self) -> dex.StepList[ListAccountsInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
