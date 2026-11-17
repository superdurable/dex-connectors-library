# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one mini app report through a statically named Dex connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.douyin import (
    Connection,
    GetBehaviorSummaryInput,
    GetBehaviorSummaryOutput,
    new_get_behavior_summary_step,
)

CONNECTION_NAME = "douyin"
_OUTCOME = dex.Attribute("douyin-get-behavior-summary-outcome", GetBehaviorSummaryOutput)


def map_to_operation_input(input: GetBehaviorSummaryInput) -> GetBehaviorSummaryInput:
    """Pass the completed report date to the provider Query."""
    return input


# dex:group group-id:get-behavior-summary group-label:"Get behavior summary"
# dex:explanation text:"Start the Douyin mini app report read."
class _StartRequest(dex.Step[GetBehaviorSummaryInput]):
    def get_step_type(self) -> str:
        return "StartGetBehaviorSummary"

    def execute(self, context: dex.Context, input: GetBehaviorSummaryInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallGetBehaviorSummary"), input)


# dex:group group-id:get-behavior-summary group-label:"Get behavior summary"
# dex:explanation text:"Persist behavior summary, then complete the Flow."
class _CompleteResult(dex.Step[QueryResult[GetBehaviorSummaryOutput]]):
    def get_step_type(self) -> str:
        return "CompleteGetBehaviorSummary"

    def execute(self, context: dex.Context, input: QueryResult[GetBehaviorSummaryOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class GetBehaviorSummaryFlow(dex.Flow[GetBehaviorSummaryInput]):
    """Read a mini app report from the static `douyin` connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("behavior summary query requires the static douyin connection")
        self._start = _StartRequest()
        self._complete = _CompleteResult()
        self._read = new_get_behavior_summary_step(
            step_type="CallGetBehaviorSummary",
            step_input_type=GetBehaviorSummaryInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "Douyin",
                "explanation": "Call Douyin's getBehaviorSummary operation.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable type shown in Dex Web."""
        return "DouyinGetBehaviorSummary"

    def get_steps(self) -> dex.StepList[GetBehaviorSummaryInput]:
        """Register the start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._read, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the aggregate behavior counts."""
        return dex.PersistenceSchema.of(_OUTCOME)
