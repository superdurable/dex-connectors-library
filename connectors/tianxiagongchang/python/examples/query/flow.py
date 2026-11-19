# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one Tianxia Gongchang response through a durable Dex Query Step."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.tianxiagongchang import (
    Connection,
    GetAccountStatusInput,
    GetAccountStatusOutput,
    new_get_account_status_step,
)

CONNECTION_NAME = "tianxiagongchang"
_RESULT = dex.Attribute("tianxiagongchang-query-result", GetAccountStatusOutput)


def map_to_operation_input(input: GetAccountStatusInput) -> GetAccountStatusInput:
    """Pass the typed request into the provider Query."""
    return input


# dex:group group-id:query group-label:"Provider query"
# dex:explanation text:"Start the Tianxia Gongchang provider Query."
class _StartQuery(dex.Step[GetAccountStatusInput]):
    def get_step_type(self) -> str:
        return "StartGetAccountStatusQuery"

    def execute(self, context: dex.Context, input: GetAccountStatusInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("GetAccountStatusQuery"), input)


# dex:group group-id:query group-label:"Provider query"
# dex:explanation text:"Persist the validated Tianxia Gongchang Query result."
class _CompleteQuery(dex.Step[QueryResult[GetAccountStatusOutput]]):
    def get_step_type(self) -> str:
        return "CompleteGetAccountStatusQuery"

    def execute(self, context: dex.Context, input: QueryResult[GetAccountStatusOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class TianxiaGongchangGetAccountStatusFlow(dex.Flow[GetAccountStatusInput]):
    """Run the Tianxia Gongchang Query with the static named connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Tianxia Gongchang Query requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._query = new_get_account_status_step(
            step_type="GetAccountStatusQuery",
            step_input_type=GetAccountStatusInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "Tianxia Gongchang",
                "explanation": "Call Tianxia Gongchang GetAccountStatus using the configured credential.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            retrieved=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity for Worker registration."""
        return "TianxiaGongchangGetAccountStatus"

    def get_steps(self) -> dex.StepList[GetAccountStatusInput]:
        """Register the start, Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the Query output for later reads."""
        return dex.PersistenceSchema.of(_RESULT)
