# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable ApiZero account-information Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.apizero import (
    Connection,
    GetAccountInfoInput,
    GetAccountInfoOutput,
    new_get_account_info_step,
)

CONNECTION_NAME = "apizero"
_RESULT = dex.Attribute("apizero-account-info", GetAccountInfoOutput)


def map_to_operation_input(request: GetAccountInfoInput) -> GetAccountInfoInput:
    """Forward the empty account-information input."""
    return request


# dex:group group-id:apizero group-label:"ApiZero"
# dex:explanation text:"Start the ApiZero account-information request."
class _StartAccountInfo(dex.Step[GetAccountInfoInput]):
    def get_step_type(self) -> str:
        return "StartApiZeroAccountInfo"

    def execute(self, context: dex.Context, input: GetAccountInfoInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallApiZero"), input)


# dex:group group-id:apizero group-label:"ApiZero"
# dex:explanation text:"Persist account information and complete the Flow."
class _CompleteAccountInfo(dex.Step[QueryResult[GetAccountInfoOutput]]):
    def get_step_type(self) -> str:
        return "CompleteApiZeroAccountInfo"

    def execute(self, context: dex.Context, input: QueryResult[GetAccountInfoOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class AccountInfoFlow(dex.Flow[GetAccountInfoInput]):
    """Read the current Key's account information through the static ApiZero connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("AccountInfoFlow requires the static ApiZero connection")
        self._start = _StartAccountInfo()
        self._complete = _CompleteAccountInfo()
        self._call = new_get_account_info_step(
            step_type="CallApiZero",
            step_input_type=GetAccountInfoInput,
            annotations={
                "groupId": "apizero",
                "groupLabel": "ApiZero",
                "explanation": "Read the current Key's wallet balance or prepaid quota.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable account-information Flow identity."""
        return "ApiZeroAccountInfo"

    def get_steps(self) -> dex.StepList[GetAccountInfoInput]:
        """Register the account-information Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the account-information result."""
        return dex.PersistenceSchema.of(_RESULT)
