# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable SendGrid demo Flow using the getStats Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.sendgrid import Connection, GetStatsInput, StatsOutput, new_get_stats_step

FLOW_TYPE = "SendGridDemo"
CONNECTION_NAME = "sendgrid"
_OPERATION_STEP_TYPE = "CallGetStats"
_OUTCOME = dex.Attribute("sendgrid-demo-outcome", StatsOutput)


def map_to_operation_input(input: GetStatsInput) -> GetStatsInput:
    """Request the statistics range unchanged."""
    return input


# dex:group group-id:request group-label:"Request"
# dex:explanation text:"Pass the typed start input to the provider Step."
class _StartRequest(dex.Step[GetStatsInput]):
    def get_step_type(self) -> str:
        return "StartGetStats"

    def execute(self, context: dex.Context, input: GetStatsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef(_OPERATION_STEP_TYPE), input)


# dex:group group-id:result group-label:"Result"
# dex:explanation text:"Complete the Flow with the provider's typed result."
class _CompleteResult(dex.Step[QueryResult[StatsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteGetStats"

    def execute(self, context: dex.Context, input: QueryResult[StatsOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class DemoFlow(dex.Flow[GetStatsInput]):
    """Read statistics through the static sendgrid connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("demo requires the static sendgrid connection")
        self._start = _StartRequest()
        self._complete = _CompleteResult()
        self._read = new_get_stats_step(
            step_type=_OPERATION_STEP_TYPE,
            step_input_type=GetStatsInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "SendGrid",
                "explanation": "Call the provider's getStats operation.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[GetStatsInput]:
        """Register the start, statistics read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._read, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the statistics."""
        return dex.PersistenceSchema.of(_OUTCOME)
