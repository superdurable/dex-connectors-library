# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Alibaba Cloud ECS regions Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.alibaba import (
    Connection,
    DescribeRegionsInput,
    DescribeRegionsOutput,
    new_describe_regions_step,
)

CONNECTION_NAME = "alibaba"
_RESULT = dex.Attribute("alibaba-ecs-regions", DescribeRegionsOutput)


def map_to_operation_input(request: DescribeRegionsInput) -> DescribeRegionsInput:
    """Forward the empty ECS request."""
    return request


# dex:group group-id:alibaba group-label:"Alibaba Cloud"
# dex:explanation text:"Start the ECS DescribeRegions request."
class _StartRegions(dex.Step[DescribeRegionsInput]):
    def get_step_type(self) -> str:
        return "StartAlibabaRegions"

    def execute(self, context: dex.Context, input: DescribeRegionsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallAlibabaRegions"), input)


# dex:group group-id:alibaba group-label:"Alibaba Cloud"
# dex:explanation text:"Persist the ECS region list and complete the Flow."
class _CompleteRegions(dex.Step[QueryResult[DescribeRegionsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteAlibabaRegions"

    def execute(self, context: dex.Context, input: QueryResult[DescribeRegionsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class RegionsFlow(dex.Flow[DescribeRegionsInput]):
    """List ECS regions through the static Alibaba Cloud connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("RegionsFlow requires the static Alibaba Cloud connection")
        self._start = _StartRegions()
        self._complete = _CompleteRegions()
        self._call = new_describe_regions_step(
            step_type="CallAlibabaRegions",
            step_input_type=DescribeRegionsInput,
            annotations={
                "groupId": "alibaba",
                "groupLabel": "Alibaba Cloud",
                "explanation": "List ECS region IDs, endpoints, and sale status.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            fetched=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable ECS regions Flow identity."""
        return "AlibabaCloudRegions"

    def get_steps(self) -> dex.StepList[DescribeRegionsInput]:
        """Register the ECS region Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the ECS region list."""
        return dex.PersistenceSchema.of(_RESULT)
