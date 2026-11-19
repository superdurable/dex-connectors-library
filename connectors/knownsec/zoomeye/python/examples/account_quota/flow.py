# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable ZoomEye getAccountQuota Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.knownsec_zoomeye import (
    Connection,
    GetAccountQuotaInput,
    GetAccountQuotaOutput,
    new_get_account_quota_step,
)

CONNECTION_NAME = "zoomeye"
_RESULT = dex.Attribute("knownsec_zoomeye-result", GetAccountQuotaOutput)


def map_to_operation_input(request: GetAccountQuotaInput) -> GetAccountQuotaInput:
    """Forward the provider request."""
    return request


# dex:group group-id:knownsec-zoomeye group-label:"ZoomEye"
# dex:explanation text:"Start the ZoomEye query."
class _StartQuery(dex.Step[GetAccountQuotaInput]):
    def execute(self, context: dex.Context, input: GetAccountQuotaInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:knownsec-zoomeye group-label:"ZoomEye"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[GetAccountQuotaOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[GetAccountQuotaOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class GetAccountQuotaFlow(dex.Flow[GetAccountQuotaInput]):
    """Run one ZoomEye query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("ZoomEye Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_get_account_quota_step(
            step_type="CallProvider",
            step_input_type=GetAccountQuotaInput,
            annotations={
                "groupId": "knownsec-zoomeye",
                "groupLabel": "ZoomEye",
                "explanation": "Run ZoomEye getAccountQuota.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            fetched=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "GetAccountQuotaZoomEye"

    def get_steps(self) -> dex.StepList[GetAccountQuotaInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
