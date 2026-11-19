# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable PingCAP project-list Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.pingcap import (
    Connection,
    ListProjectsInput,
    ListProjectsOutput,
    new_list_projects_step,
)

CONNECTION_NAME = "pingcap"
_RESULT = dex.Attribute("pingcap-project-list", ListProjectsOutput)


def map_to_operation_input(request: ListProjectsInput) -> ListProjectsInput:
    """Forward the project page request to PingCAP."""
    return request


# dex:group group-id:pingcap group-label:"PingCAP"
# dex:explanation text:"Start the PingCAP project-list request."
class _StartProfile(dex.Step[ListProjectsInput]):
    def get_step_type(self) -> str:
        return "StartPingCAPListProjectsOutput"

    def execute(self, context: dex.Context, input: ListProjectsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallPingCAP"), input)


# dex:group group-id:pingcap group-label:"PingCAP"
# dex:explanation text:"Persist project page and complete the Flow."
class _CompleteProfile(dex.Step[QueryResult[ListProjectsOutput]]):
    def get_step_type(self) -> str:
        return "CompletePingCAPListProjectsOutput"

    def execute(self, context: dex.Context, input: QueryResult[ListProjectsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListProjectsOutputFlow(dex.Flow[ListProjectsInput]):
    """Read one project page through the static PingCAP connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("ListProjectsOutputFlow requires the static PingCAP connection")
        self._start = _StartProfile()
        self._complete = _CompleteProfile()
        self._call = new_list_projects_step(
            step_type="CallPingCAP",
            step_input_type=ListProjectsInput,
            annotations={
                "groupId": "pingcap",
                "groupLabel": "PingCAP",
                "explanation": "List TiDB Cloud projects.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable project-list Flow identity."""
        return "PingCAPListProjectsOutput"

    def get_steps(self) -> dex.StepList[ListProjectsInput]:
        """Register the project list Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the project-list result."""
        return dex.PersistenceSchema.of(_RESULT)
