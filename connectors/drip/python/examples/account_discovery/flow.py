# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Drip account discovery Flow using the listAccounts Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.drip import (
    AccountsList,
    Connection,
    ListAccountsInput,
    new_list_accounts_step,
)

FLOW_TYPE = "DripAccountDiscovery"
CONNECTION_NAME = "drip"
_CALL_STEP_TYPE = "CallListAccounts"
_RESULT = dex.Attribute("drip-accounts-list", AccountsList)


def map_to_operation_input(request: ListAccountsInput) -> ListAccountsInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:drip group-label:"Drip"
# dex:explanation text:"Start the Drip request."
class _StartRequest(dex.Step[ListAccountsInput]):
    def get_step_type(self) -> str:
        return "StartListAccounts"

    def execute(self, context: dex.Context, input: ListAccountsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListAccounts"), input)


# dex:group group-id:drip group-label:"Drip"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[AccountsList]]):
    def get_step_type(self) -> str:
        return "CompleteListAccounts"

    def execute(self, context: dex.Context, input: QueryResult[AccountsList]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class AccountDiscoveryFlow(dex.Flow[ListAccountsInput]):
    """Run one listAccounts read through the static drip connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the account discovery requires the static drip connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_accounts_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListAccountsInput,
            annotations={
                "groupId": "drip",
                "groupLabel": "Drip",
                "explanation": "List accounts accessible to the connected Drip account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "DripAccountDiscovery"

    def get_steps(self) -> dex.StepList[ListAccountsInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
