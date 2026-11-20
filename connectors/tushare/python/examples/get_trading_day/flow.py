# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Tushare getTradingDay Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.tushare import (
    Connection,
    GetTradingDayInput,
    GetTradingDayOutput,
    new_get_trading_day_step,
)

CONNECTION_NAME = "tushare"
_RESULT = dex.Attribute("tushare-result", GetTradingDayOutput)


def map_to_operation_input(request: GetTradingDayInput) -> GetTradingDayInput:
    """Forward the provider request."""
    return request


# dex:group group-id:tushare group-label:"Tushare"
# dex:explanation text:"Start the Tushare query."
class _StartQuery(dex.Step[GetTradingDayInput]):
    def execute(self, context: dex.Context, input: GetTradingDayInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:tushare group-label:"Tushare"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[GetTradingDayOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[GetTradingDayOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class GetTradingDayFlow(dex.Flow[GetTradingDayInput]):
    """Run one Tushare query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Tushare Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_get_trading_day_step(
            step_type="CallProvider",
            step_input_type=GetTradingDayInput,
            annotations={
                "groupId": "tushare",
                "groupLabel": "Tushare",
                "explanation": "Run Tushare getTradingDay.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "GetTradingDayTushare"

    def get_steps(self) -> dex.StepList[GetTradingDayInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
