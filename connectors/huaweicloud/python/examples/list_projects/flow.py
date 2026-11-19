# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read huaweicloud data through the static Dex connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.huaweicloud import (
    Connection,
    ListAccessibleProjectsInput,
    ListAccessibleProjectsOutput,
    new_list_accessible_projects_step,
)

CONNECTION_NAME = "huaweicloud"
_OUTCOME = dex.Attribute("huaweicloud-list-projects-result", ListAccessibleProjectsOutput)


def map_to_operation_input(input: ListAccessibleProjectsInput) -> ListAccessibleProjectsInput:
    """Pass the typed request to the provider Query."""
    return input


# dex:group group-id:list-projects group-label:"ListProjects"
# dex:explanation text:"Start the huaweicloud Query."
class _StartRequest(dex.Step[ListAccessibleProjectsInput]):
    def get_step_type(self) -> str:
        return "StartListProjects"

    def execute(self, context: dex.Context, input: ListAccessibleProjectsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListProjects"), input)


# dex:group group-id:list-projects group-label:"ListProjects"
# dex:explanation text:"Persist the provider result and complete the Flow."
class _CompleteResult(dex.Step[QueryResult[ListAccessibleProjectsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteListProjects"

    def execute(
        self, context: dex.Context, input: QueryResult[ListAccessibleProjectsOutput]
    ) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListProjectsFlow(dex.Flow[ListAccessibleProjectsInput]):
    """Run listAccessibleProjects using the static `huaweicloud` connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("huaweicloud example requires the static connection")
        self._start = _StartRequest()
        self._complete = _CompleteResult()
        self._query = new_list_accessible_projects_step(
            step_type="CallListProjects",
            step_input_type=ListAccessibleProjectsInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "huaweicloud",
                "explanation": "Call the huaweicloud listAccessibleProjects Query.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "HuaweicloudListProjects"

    def get_steps(self) -> dex.StepList[ListAccessibleProjectsInput]:
        """Register the start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the typed provider output."""
        return dex.PersistenceSchema.of(_OUTCOME)
