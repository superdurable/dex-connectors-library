# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Huoban workspace catalog Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.huoban import (
    Connection,
    ListSpacesInput,
    ListSpacesOutput,
    new_list_spaces_step,
)

CONNECTION_NAME = "huoban"
_RESULT = dex.Attribute("huoban-space-list", ListSpacesOutput)


def map_to_operation_input(request: ListSpacesInput) -> ListSpacesInput:
    """Forward the empty space-list request."""
    return request


# dex:group group-id:huoban group-label:"Huoban"
# dex:explanation text:"Start the Huoban space-list request."
class _StartListSpaces(dex.Step[ListSpacesInput]):
    def get_step_type(self) -> str:
        return "StartHuobanSpaceList"

    def execute(self, context: dex.Context, input: ListSpacesInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallHuoban"), input)


# dex:group group-id:huoban group-label:"Huoban"
# dex:explanation text:"Persist workspace IDs and names and complete the Flow."
class _CompleteListSpaces(dex.Step[QueryResult[ListSpacesOutput]]):
    def get_step_type(self) -> str:
        return "CompleteHuobanSpaceList"

    def execute(self, context: dex.Context, input: QueryResult[ListSpacesOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class SpaceListFlow(dex.Flow[ListSpacesInput]):
    """List workspaces through the static Huoban connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("SpaceListFlow requires the static Huoban connection")
        self._start = _StartListSpaces()
        self._complete = _CompleteListSpaces()
        self._call = new_list_spaces_step(
            step_type="CallHuoban",
            step_input_type=ListSpacesInput,
            annotations={
                "groupId": "huoban",
                "groupLabel": "Huoban",
                "explanation": "List workspace IDs and names visible to the Huoban Key.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable space-list Flow identity."""
        return "HUOBANSpaceList"

    def get_steps(self) -> dex.StepList[ListSpacesInput]:
        """Register the space-list Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the bounded space-list result."""
        return dex.PersistenceSchema.of(_RESULT)
