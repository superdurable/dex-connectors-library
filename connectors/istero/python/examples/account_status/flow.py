# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable IsTero account status Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.istero import (
    Connection,
    GetAccountStatusInput,
    GetAccountStatusOutput,
    new_get_account_status_step,
)

CONNECTION_NAME = "istero"
_RESULT = dex.Attribute("istero-account-status", GetAccountStatusOutput)


def map_to_operation_input(request: GetAccountStatusInput) -> GetAccountStatusInput:
    """Forward the requested account status."""
    return request


# dex:group group-id:istero group-label:"IsTero"
# dex:explanation text:"Start one IsTero account status query."
class _StartAccountStatus(dex.Step[GetAccountStatusInput]):
    def execute(self, context: dex.Context, input: GetAccountStatusInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallIsTero"), input)


# dex:group group-id:istero group-label:"IsTero"
# dex:explanation text:"Persist coarse account status before completion."
class _CompleteAccountStatus(dex.Step[QueryResult[GetAccountStatusOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[GetAccountStatusOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class AccountStatusFlow(dex.Flow[GetAccountStatusInput]):
    """Read one account status through the static IsTero connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("AccountStatusFlow requires the static IsTero connection")
        self._start = _StartAccountStatus()
        self._complete = _CompleteAccountStatus()
        self._call = new_get_account_status_step(
            step_type="CallIsTero",
            step_input_type=GetAccountStatusInput,
            annotations={
                "groupId": "istero",
                "groupLabel": "IsTero",
                "explanation": "Read used and remaining daily ISAS calls.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            retrieved=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable account status Flow identity."""
        return "IsTeroAccountStatus"

    def get_steps(self) -> dex.StepList[GetAccountStatusInput]:
        """Register the account status Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist coarse account status only."""
        return dex.PersistenceSchema.of(_RESULT)
