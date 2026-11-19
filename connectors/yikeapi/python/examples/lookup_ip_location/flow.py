# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Yikeapi lookupIPLocation Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.yikeapi import (
    Connection,
    LookupIPLocationInput,
    LookupIPLocationOutput,
    new_lookup_ip_location_step,
)

CONNECTION_NAME = "yikeapi"
_RESULT = dex.Attribute("yikeapi-result", LookupIPLocationOutput)


def map_to_operation_input(request: LookupIPLocationInput) -> LookupIPLocationInput:
    """Forward the provider request."""
    return request


# dex:group group-id:yikeapi group-label:"Yikeapi"
# dex:explanation text:"Start the Yikeapi query."
class _StartQuery(dex.Step[LookupIPLocationInput]):
    def execute(self, context: dex.Context, input: LookupIPLocationInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:yikeapi group-label:"Yikeapi"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[LookupIPLocationOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[LookupIPLocationOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class LookupIPLocationFlow(dex.Flow[LookupIPLocationInput]):
    """Run one Yikeapi query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Yikeapi Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_lookup_ip_location_step(
            step_type="CallProvider",
            step_input_type=LookupIPLocationInput,
            annotations={
                "groupId": "yikeapi",
                "groupLabel": "Yikeapi",
                "explanation": "Run Yikeapi lookupIPLocation.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "LookupIPLocationYikeapi"

    def get_steps(self) -> dex.StepList[LookupIPLocationInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
