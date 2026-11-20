# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Useasy listAgents Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.useasy import (
    Connection,
    ListAgentsInput,
    ListAgentsOutput,
    new_list_agents_step,
)

CONNECTION_NAME = "useasy"
_RESULT = dex.Attribute("useasy-result", ListAgentsOutput)


def map_to_operation_input(request: ListAgentsInput) -> ListAgentsInput:
    """Forward the provider request."""
    return request


# dex:group group-id:useasy group-label:"Useasy"
# dex:explanation text:"Start the Useasy query."
class _StartQuery(dex.Step[ListAgentsInput]):
    def execute(self, context: dex.Context, input: ListAgentsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:useasy group-label:"Useasy"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[ListAgentsOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[ListAgentsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListAgentsFlow(dex.Flow[ListAgentsInput]):
    """Run one Useasy query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Useasy Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_list_agents_step(
            step_type="CallProvider",
            step_input_type=ListAgentsInput,
            annotations={
                "groupId": "useasy",
                "groupLabel": "Useasy",
                "explanation": "Run Useasy listAgents.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "ListAgentsUseasy"

    def get_steps(self) -> dex.StepList[ListAgentsInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
