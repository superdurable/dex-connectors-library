# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Yunda getWaybillBalance Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.yunda import (
    Connection,
    GetWaybillBalanceInput,
    GetWaybillBalanceOutput,
    new_get_waybill_balance_step,
)

CONNECTION_NAME = "yunda"
_RESULT = dex.Attribute("yunda-result", GetWaybillBalanceOutput)


def map_to_operation_input(request: GetWaybillBalanceInput) -> GetWaybillBalanceInput:
    """Forward the provider request."""
    return request


# dex:group group-id:yunda group-label:"Yunda"
# dex:explanation text:"Start the Yunda query."
class _StartQuery(dex.Step[GetWaybillBalanceInput]):
    def execute(self, context: dex.Context, input: GetWaybillBalanceInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:yunda group-label:"Yunda"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[GetWaybillBalanceOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[GetWaybillBalanceOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class GetWaybillBalanceFlow(dex.Flow[GetWaybillBalanceInput]):
    """Run one Yunda query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Yunda Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_get_waybill_balance_step(
            step_type="CallProvider",
            step_input_type=GetWaybillBalanceInput,
            annotations={
                "groupId": "yunda",
                "groupLabel": "Yunda",
                "explanation": "Run Yunda getWaybillBalance.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "GetWaybillBalanceYunda"

    def get_steps(self) -> dex.StepList[GetWaybillBalanceInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
