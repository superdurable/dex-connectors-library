# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable TianAPI getHolidayStatus Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.tianapi import (
    Connection,
    GetHolidayStatusInput,
    GetHolidayStatusOutput,
    new_get_holiday_status_step,
)

CONNECTION_NAME = "tianapi"
_RESULT = dex.Attribute("tianapi-result", GetHolidayStatusOutput)


def map_to_operation_input(request: GetHolidayStatusInput) -> GetHolidayStatusInput:
    """Forward the provider request."""
    return request


# dex:group group-id:tianapi group-label:"TianAPI"
# dex:explanation text:"Start the TianAPI query."
class _StartQuery(dex.Step[GetHolidayStatusInput]):
    def execute(self, context: dex.Context, input: GetHolidayStatusInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:tianapi group-label:"TianAPI"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[GetHolidayStatusOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[GetHolidayStatusOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class GetHolidayStatusFlow(dex.Flow[GetHolidayStatusInput]):
    """Run one TianAPI query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("TianAPI Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_get_holiday_status_step(
            step_type="CallProvider",
            step_input_type=GetHolidayStatusInput,
            annotations={
                "groupId": "tianapi",
                "groupLabel": "TianAPI",
                "explanation": "Run TianAPI getHolidayStatus.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "GetHolidayStatusTianapi"

    def get_steps(self) -> dex.StepList[GetHolidayStatusInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
