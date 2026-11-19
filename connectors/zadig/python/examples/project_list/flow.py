# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read zadig through one durable provider Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.zadig import Connection, ListProjectsInput, ListProjectsOutput, new_list_projects_step

CONNECTION_NAME = "zadig"
_OUTCOME = dex.Attribute("zadig-list_projects-outcome", ListProjectsOutput)


def map_to_operation_input(input: ListProjectsInput) -> ListProjectsInput:
    """Forward the typed Flow input to the provider Query."""
    return input


# dex:group group-id:read group-label:"Provider read"
# dex:explanation text:"Begin the zadig read with the configured connection."
class _Start(dex.Step[ListProjectsInput]):
    def get_step_type(self) -> str:
        return "StartListProjects"

    def execute(self, context: dex.Context, input: ListProjectsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("ListProjectsQuery"), input)


# dex:group group-id:read group-label:"Provider read"
# dex:explanation text:"Persist the provider result and complete the Flow."
class _Complete(dex.Step[QueryResult[ListProjectsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteListProjects"

    def execute(self, context: dex.Context, input: QueryResult[ListProjectsOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListProjectsFlow(dex.Flow[ListProjectsInput]):
    """Durably read zadig with its static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("zadig Flow requires its static connection")
        self._start = _Start()
        self._complete = _Complete()
        self._query = new_list_projects_step(
            step_type="ListProjectsQuery",
            step_input_type=ListProjectsInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "zadig",
                "explanation": "Call zadig listProjects.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity shown in Dex Web."""
        return "ZadigListProjects"

    def get_steps(self) -> dex.StepList[ListProjectsInput]:
        """Register the start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable provider result Attribute."""
        return dex.PersistenceSchema.of(_OUTCOME)
