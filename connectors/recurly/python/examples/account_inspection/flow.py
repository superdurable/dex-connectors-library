# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Recurly account inspection Flow using getAccount."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.recurly import (
    AccountIdentity,
    Connection,
    GetAccountInput,
    new_get_account_step,
)

FLOW_TYPE = "RecurlyAccountInspectionPython"
CONNECTION_NAME = "recurly"
_CALL_STEP_TYPE = "CallRecurly"
_RESULT = dex.Attribute("recurly-account", AccountIdentity)


def map_to_operation_input(request: GetAccountInput) -> GetAccountInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:recurly group-label:"Recurly"
# dex:explanation text:"Start the Recurly request."
class _StartRequest(dex.Step[GetAccountInput]):
    def get_step_type(self) -> str:
        return "StartRecurlyRequest"

    def execute(self, context: dex.Context, input: GetAccountInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallRecurly"), input)


# dex:group group-id:recurly group-label:"Recurly"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[AccountIdentity]]):
    def get_step_type(self) -> str:
        return "CompleteRecurlyRequest"

    def execute(self, context: dex.Context, input: QueryResult[AccountIdentity]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class AccountInspectionFlow(dex.Flow[GetAccountInput]):
    """Read one account through the static recurly connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("account inspection requires the static recurly connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_account_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetAccountInput,
            annotations={
                "groupId": "recurly",
                "groupLabel": "Recurly",
                "explanation": "Read one account from the connected Recurly account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "RecurlyAccountInspectionPython"

    def get_steps(self) -> dex.StepList[GetAccountInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
