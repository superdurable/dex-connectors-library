# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one Oceanbase response through a durable Dex Query Step."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.oceanbase import (
    Connection,
    ListInstancesInput,
    ListInstancesOutput,
    new_list_instances_step,
)

CONNECTION_NAME = "oceanbase"
_RESULT = dex.Attribute("oceanbase-query-result", ListInstancesOutput)


def map_to_operation_input(input: ListInstancesInput) -> ListInstancesInput:
    """Pass the typed request into the provider Query."""
    return input


# dex:group group-id:query group-label:"Provider query"
# dex:explanation text:"Start the Oceanbase provider Query."
class _StartQuery(dex.Step[ListInstancesInput]):
    def get_step_type(self) -> str:
        return "StartListInstancesQuery"

    def execute(self, context: dex.Context, input: ListInstancesInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("ListInstancesQuery"), input)


# dex:group group-id:query group-label:"Provider query"
# dex:explanation text:"Persist the validated Oceanbase Query result."
class _CompleteQuery(dex.Step[QueryResult[ListInstancesOutput]]):
    def get_step_type(self) -> str:
        return "CompleteListInstancesQuery"

    def execute(self, context: dex.Context, input: QueryResult[ListInstancesOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class OceanbaseListInstancesFlow(dex.Flow[ListInstancesInput]):
    """Run the Oceanbase Query with the static named connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Oceanbase Query requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._query = new_list_instances_step(
            step_type="ListInstancesQuery",
            step_input_type=ListInstancesInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "Oceanbase",
                "explanation": "Call Oceanbase ListInstances using the configured credential.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity for Worker registration."""
        return "OceanbaseListInstances"

    def get_steps(self) -> dex.StepList[ListInstancesInput]:
        """Register the start, Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the Query output for later reads."""
        return dex.PersistenceSchema.of(_RESULT)
