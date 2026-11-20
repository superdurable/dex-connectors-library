# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Quanshi listRooms Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.quanshi import (
    Connection,
    ListRoomsInput,
    ListRoomsOutput,
    new_list_rooms_step,
)

CONNECTION_NAME = "quanshi"
_RESULT = dex.Attribute("quanshi-result", ListRoomsOutput)


def map_to_operation_input(request: ListRoomsInput) -> ListRoomsInput:
    """Forward the provider request."""
    return request


# dex:group group-id:quanshi group-label:"Quanshi"
# dex:explanation text:"Start the Quanshi query."
class _StartQuery(dex.Step[ListRoomsInput]):
    def execute(self, context: dex.Context, input: ListRoomsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:quanshi group-label:"Quanshi"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[ListRoomsOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[ListRoomsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListRoomsFlow(dex.Flow[ListRoomsInput]):
    """Run one Quanshi query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Quanshi Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_list_rooms_step(
            step_type="CallProvider",
            step_input_type=ListRoomsInput,
            annotations={
                "groupId": "quanshi",
                "groupLabel": "Quanshi",
                "explanation": "Run Quanshi listRooms.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "ListRoomsQuanshi"

    def get_steps(self) -> dex.StepList[ListRoomsInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
