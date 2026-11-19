# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read likelic data through the static Dex connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.likelic import Connection, ListRoomsInput, ListRoomsOutput, new_list_rooms_step

CONNECTION_NAME = "likelic"
_OUTCOME = dex.Attribute("likelic-list_rooms-result", ListRoomsOutput)


def map_to_operation_input(input: ListRoomsInput) -> ListRoomsInput:
    """Pass the typed request to the provider Query."""
    return input


# dex:group group-id:list-rooms group-label:"ListRooms"
# dex:explanation text:"Start the likelic Query."
class _StartRequest(dex.Step[ListRoomsInput]):
    def get_step_type(self) -> str:
        return "StartListRooms"

    def execute(self, context: dex.Context, input: ListRoomsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListRooms"), input)


# dex:group group-id:list-rooms group-label:"ListRooms"
# dex:explanation text:"Persist the provider result and complete the Flow."
class _CompleteResult(dex.Step[QueryResult[ListRoomsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteListRooms"

    def execute(self, context: dex.Context, input: QueryResult[ListRoomsOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListRoomsFlow(dex.Flow[ListRoomsInput]):
    """Run listRooms using the static `likelic` connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("likelic example requires the static connection")
        self._start = _StartRequest()
        self._complete = _CompleteResult()
        self._query = new_list_rooms_step(
            step_type="CallListRooms",
            step_input_type=ListRoomsInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "likelic",
                "explanation": "Call the likelic listRooms Query.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "LikelicListRooms"

    def get_steps(self) -> dex.StepList[ListRoomsInput]:
        """Register the start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the typed provider output."""
        return dex.PersistenceSchema.of(_OUTCOME)
