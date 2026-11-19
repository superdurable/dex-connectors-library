# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one Tingyun response through a durable Dex Query Step."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.tingyun import (
    Connection,
    ListApplicationsInput,
    ListApplicationsOutput,
    new_list_applications_step,
)

CONNECTION_NAME = "tingyun"
_RESULT = dex.Attribute("tingyun-query-result", ListApplicationsOutput)


def map_to_operation_input(input: ListApplicationsInput) -> ListApplicationsInput:
    """Pass the typed request into the provider Query."""
    return input


# dex:group group-id:query group-label:"Provider query"
# dex:explanation text:"Start the Tingyun provider Query."
class _StartQuery(dex.Step[ListApplicationsInput]):
    def get_step_type(self) -> str:
        return "StartListApplicationsQuery"

    def execute(self, context: dex.Context, input: ListApplicationsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("ListApplicationsQuery"), input)


# dex:group group-id:query group-label:"Provider query"
# dex:explanation text:"Persist the validated Tingyun Query result."
class _CompleteQuery(dex.Step[QueryResult[ListApplicationsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteListApplicationsQuery"

    def execute(self, context: dex.Context, input: QueryResult[ListApplicationsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class TingyunListApplicationsFlow(dex.Flow[ListApplicationsInput]):
    """Run the Tingyun Query with the static named connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Tingyun Query requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._query = new_list_applications_step(
            step_type="ListApplicationsQuery",
            step_input_type=ListApplicationsInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "Tingyun",
                "explanation": "Call Tingyun ListApplications using the configured credential.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity for Worker registration."""
        return "TingyunListApplications"

    def get_steps(self) -> dex.StepList[ListApplicationsInput]:
        """Register the start, Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the Query output for later reads."""
        return dex.PersistenceSchema.of(_RESULT)
