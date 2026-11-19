# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Query a known Feihan recharge order's status."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.feihan import (
    Connection,
    QueryRechargeOrderInput,
    RechargeOrderResult,
    new_query_recharge_order_step,
)

CONNECTION_NAME = "feihan"
_OUTCOME = dex.Attribute("feihan-recharge-order-outcome", RechargeOrderResult)


def map_to_operation_input(input: QueryRechargeOrderInput) -> QueryRechargeOrderInput:
    """Pass the requested value to the provider Query."""
    return input


# dex:group group-id:query group-label:"Provider query"
# dex:explanation text:"Start the provider query."
class _StartQuery(dex.Step[QueryRechargeOrderInput]):
    def get_step_type(self) -> str:
        return "StartRechargeOrder"

    def execute(self, context: dex.Context, input: QueryRechargeOrderInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("QueryRechargeOrderQuery"), input)


# dex:group group-id:query group-label:"Provider query"
# dex:explanation text:"Persist the provider result and complete the Flow."
class _CompleteQuery(dex.Step[QueryResult[RechargeOrderResult]]):
    def get_step_type(self) -> str:
        return "CompleteRechargeOrder"

    def execute(self, context: dex.Context, input: QueryResult[RechargeOrderResult]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class FeihanRechargeOrderFlow(dex.Flow[QueryRechargeOrderInput]):
    """Run feihan's Query with its static project connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("example requires the static feihan connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._query = new_query_recharge_order_step(
            step_type="QueryRechargeOrderQuery",
            step_input_type=QueryRechargeOrderInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "feihan",
                "explanation": "Call feihan QueryRechargeOrder.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "FeihanRechargeOrderFlow"

    def get_steps(self) -> dex.StepList[QueryRechargeOrderInput]:
        """Register the start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable result Attribute."""
        return dex.PersistenceSchema.of(_OUTCOME)
