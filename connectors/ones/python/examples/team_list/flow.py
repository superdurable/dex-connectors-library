# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read ones through one durable provider Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.ones import Connection, ListTeamsInput, ListTeamsOutput, new_list_teams_step

CONNECTION_NAME = "ones"
_OUTCOME = dex.Attribute("ones-list_teams-outcome", ListTeamsOutput)


def map_to_operation_input(input: ListTeamsInput) -> ListTeamsInput:
    """Forward the typed Flow input to the provider Query."""
    return input


# dex:group group-id:read group-label:"Provider read"
# dex:explanation text:"Begin the ones read with the configured connection."
class _Start(dex.Step[ListTeamsInput]):
    def get_step_type(self) -> str:
        return "StartListTeams"

    def execute(self, context: dex.Context, input: ListTeamsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("ListTeamsQuery"), input)


# dex:group group-id:read group-label:"Provider read"
# dex:explanation text:"Persist the provider result and complete the Flow."
class _Complete(dex.Step[QueryResult[ListTeamsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteListTeams"

    def execute(self, context: dex.Context, input: QueryResult[ListTeamsOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListTeamsFlow(dex.Flow[ListTeamsInput]):
    """Durably read ones with its static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("ones Flow requires its static connection")
        self._start = _Start()
        self._complete = _Complete()
        self._query = new_list_teams_step(
            step_type="ListTeamsQuery",
            step_input_type=ListTeamsInput,
            annotations={"groupId": "provider", "groupLabel": "ones", "explanation": "Call ones listTeams."},
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity shown in Dex Web."""
        return "OnesListTeams"

    def get_steps(self) -> dex.StepList[ListTeamsInput]:
        """Register the start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable provider result Attribute."""
        return dex.PersistenceSchema.of(_OUTCOME)
