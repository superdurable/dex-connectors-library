# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Miro board inspection Flow using getBoard."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.miro import (
    BoardMetadata,
    Connection,
    GetBoardInput,
    new_get_board_step,
)

FLOW_TYPE = "MiroBoardInspectionPython"
CONNECTION_NAME = "miro"
_CALL_STEP_TYPE = "CallMiro"
_RESULT = dex.Attribute("miro-board", BoardMetadata)


def map_to_operation_input(request: GetBoardInput) -> GetBoardInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:miro group-label:"Miro"
# dex:explanation text:"Start the Miro request."
class _StartRequest(dex.Step[GetBoardInput]):
    def get_step_type(self) -> str:
        return "StartMiroRequest"

    def execute(self, context: dex.Context, input: GetBoardInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallMiro"), input)


# dex:group group-id:miro group-label:"Miro"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[BoardMetadata]]):
    def get_step_type(self) -> str:
        return "CompleteMiroRequest"

    def execute(self, context: dex.Context, input: QueryResult[BoardMetadata]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class BoardInspectionFlow(dex.Flow[GetBoardInput]):
    """Read one board through the static miro connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("board inspection requires the static miro connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_board_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetBoardInput,
            annotations={
                "groupId": "miro",
                "groupLabel": "Miro",
                "explanation": "Read one board from the connected Miro account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "MiroBoardInspectionPython"

    def get_steps(self) -> dex.StepList[GetBoardInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
