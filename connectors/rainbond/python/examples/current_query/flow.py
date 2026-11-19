# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read Rainbond provider data through a durable Dex Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.rainbond import Connection, ListTeamsInput, ListTeamsOutput, new_list_teams_step

CONNECTION_NAME = "rainbond"
_RESULT = dex.Attribute("rainbond-python-query-result", ListTeamsOutput)


def map_to_operation_input(input: ListTeamsInput) -> ListTeamsInput:
    """Forward the provider Query input."""
    return input


# dex:group group-id:rainbond group-label:"Rainbond"
# dex:explanation text:"Start the Rainbond provider Query."
class _StartQuery(dex.Step[ListTeamsInput]):
    def get_step_type(self) -> str:
        return "StartRainbond"

    def execute(self, context: dex.Context, input: ListTeamsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallRainbond"), input)


# dex:group group-id:rainbond group-label:"Rainbond"
# dex:explanation text:"Persist the Rainbond Query output and complete the Flow."
class _CompleteQuery(dex.Step[QueryResult[ListTeamsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteRainbond"

    def execute(self, context: dex.Context, input: QueryResult[ListTeamsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListTeamsFlow(dex.Flow[ListTeamsInput]):
    """Read Rainbond through the static connection and persist its result."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Rainbond Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_list_teams_step(
            step_type="CallRainbond",
            step_input_type=ListTeamsInput,
            annotations={
                "groupId": "rainbond",
                "groupLabel": "Rainbond",
                "explanation": "Read Rainbond provider data.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Rainbond Flow identity."""
        return "RainbondListTeams"

    def get_steps(self) -> dex.StepList[ListTeamsInput]:
        """Register the start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable result Attribute."""
        return dex.PersistenceSchema.of(_RESULT)
