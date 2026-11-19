# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Vika list spaces Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.vika import Connection, ListSpacesInput, SpacePage, new_list_spaces_step

CONNECTION_NAME = "vika"
_RESULT = dex.Attribute("vika-list-spaces-result", SpacePage)


def map_to_operation_input(request: ListSpacesInput) -> ListSpacesInput:
    """Forward the typed request to the provider Query."""
    return request


# dex:group group-id:vika group-label:"Vika"
# dex:explanation text:"Start the Vika list spaces request."
class _StartQuery(dex.Step[ListSpacesInput]):
    def get_step_type(self) -> str:
        return "StartVikaSpaceList"

    def execute(self, context: dex.Context, input: ListSpacesInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallVikaSpaceList"), input)


# dex:group group-id:vika group-label:"Vika"
# dex:explanation text:"Persist the provider result and complete the Flow."
class _CompleteQuery(dex.Step[QueryResult[SpacePage]]):
    def get_step_type(self) -> str:
        return "CompleteVikaSpaceList"

    def execute(self, context: dex.Context, input: QueryResult[SpacePage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class VikaSpaceListFlow(dex.Flow[ListSpacesInput]):
    """Run the list spaces Query with a static named connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("VikaSpaceListFlow requires the static Vika connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_list_spaces_step(
            step_type="CallVikaSpaceList",
            step_input_type=ListSpacesInput,
            annotations={
                "groupId": "vika",
                "groupLabel": "Vika",
                "explanation": "List spaces with the configured Vika connection.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable list spaces Flow identity."""
        return "VikaSpaceList"

    def get_steps(self) -> dex.StepList[ListSpacesInput]:
        """Register the application and connector Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the bounded list spaces result."""
        return dex.PersistenceSchema.of(_RESULT)
