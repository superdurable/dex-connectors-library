# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Summarize an Apifox project through a statically named Dex connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.apifox import (
    Connection,
    GetProjectAPISummaryInput,
    GetProjectAPISummaryOutput,
    new_get_project_api_summary_step,
)

CONNECTION_NAME = "apifox"
_OUTCOME = dex.Attribute("apifox-project-summary-outcome", GetProjectAPISummaryOutput)


def map_to_operation_input(input: GetProjectAPISummaryInput) -> GetProjectAPISummaryInput:
    """Pass the selected project ID to the Query."""
    return input


# dex:group group-id:project-summary group-label:"Project summary"
# dex:explanation text:"Start the Apifox OpenAPI summary lookup."
class _StartRequest(dex.Step[GetProjectAPISummaryInput]):
    def get_step_type(self) -> str:
        return "StartGetProjectAPISummary"

    def execute(self, context: dex.Context, input: GetProjectAPISummaryInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallGetProjectAPISummary"), input)


# dex:group group-id:project-summary group-label:"Project summary"
# dex:explanation text:"Record the project summary and complete the Flow."
class _CompleteResult(dex.Step[QueryResult[GetProjectAPISummaryOutput]]):
    def get_step_type(self) -> str:
        return "CompleteGetProjectAPISummary"

    def execute(
        self, context: dex.Context, input: QueryResult[GetProjectAPISummaryOutput]
    ) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class ProjectSummaryFlow(dex.Flow[GetProjectAPISummaryInput]):
    """Read one Apifox project's API summary through the `apifox` connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("project summary requires the static apifox connection")
        self._start = _StartRequest()
        self._complete = _CompleteResult()
        self._read = new_get_project_api_summary_step(
            step_type="CallGetProjectAPISummary",
            step_input_type=GetProjectAPISummaryInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "Apifox",
                "explanation": "Call Apifox's getProjectAPISummary operation.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity shown in Dex Web."""
        return "ApifoxProjectSummary"

    def get_steps(self) -> dex.StepList[GetProjectAPISummaryInput]:
        """Register the start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._read, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the project summary for reads."""
        return dex.PersistenceSchema.of(_OUTCOME)
