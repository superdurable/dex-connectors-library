# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Getui daily push report Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.getui import (
    Connection,
    GetDailyPushReportInput,
    GetDailyPushReportOutput,
    new_get_daily_push_report_step,
)

CONNECTION_NAME = "getui"
_RESULT = dex.Attribute("getui-daily-push-report", GetDailyPushReportOutput)


def map_to_operation_input(request: GetDailyPushReportInput) -> GetDailyPushReportInput:
    """Forward the daily push report request."""
    return request


# dex:group group-id:getui group-label:"Getui"
# dex:explanation text:"Start one Getui daily push report query."
class _StartDailyPushReport(dex.Step[GetDailyPushReportInput]):
    def execute(self, context: dex.Context, input: GetDailyPushReportInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallGetui"), input)


# dex:group group-id:getui group-label:"Getui"
# dex:explanation text:"Persist aggregate daily push totals before completion."
class _CompleteDailyPushReport(dex.Step[QueryResult[GetDailyPushReportOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[GetDailyPushReportOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class DailyPushReportFlow(dex.Flow[GetDailyPushReportInput]):
    """Read prior-day push totals through the static Getui connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("DailyPushReportFlow requires the static Getui connection")
        self._start = _StartDailyPushReport()
        self._complete = _CompleteDailyPushReport()
        self._call = new_get_daily_push_report_step(
            step_type="CallGetui",
            step_input_type=GetDailyPushReportInput,
            annotations={
                "groupId": "getui",
                "groupLabel": "Getui",
                "explanation": "Read one app's prior-day push totals.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable daily push report Flow identity."""
        return "GetuiDailyPushReport"

    def get_steps(self) -> dex.StepList[GetDailyPushReportInput]:
        """Register the daily push report Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the returned daily push report only."""
        return dex.PersistenceSchema.of(_RESULT)
