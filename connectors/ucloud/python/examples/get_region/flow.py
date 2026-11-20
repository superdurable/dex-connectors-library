# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Ucloud getRegion Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.ucloud import (
    Connection,
    GetRegionInput,
    GetRegionOutput,
    new_get_region_step,
)

CONNECTION_NAME = "ucloud"
_RESULT = dex.Attribute("ucloud-result", GetRegionOutput)


def map_to_operation_input(request: GetRegionInput) -> GetRegionInput:
    """Forward the provider request."""
    return request


# dex:group group-id:ucloud group-label:"Ucloud"
# dex:explanation text:"Start the Ucloud query."
class _StartQuery(dex.Step[GetRegionInput]):
    def execute(self, context: dex.Context, input: GetRegionInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:ucloud group-label:"Ucloud"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[GetRegionOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[GetRegionOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class GetRegionFlow(dex.Flow[GetRegionInput]):
    """Run one Ucloud query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Ucloud Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_get_region_step(
            step_type="CallProvider",
            step_input_type=GetRegionInput,
            annotations={
                "groupId": "ucloud",
                "groupLabel": "Ucloud",
                "explanation": "Run Ucloud getRegion.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "GetRegionUcloud"

    def get_steps(self) -> dex.StepList[GetRegionInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
